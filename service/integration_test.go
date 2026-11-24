// integration_test.go
package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"golang.org/x/time/rate"
)

type rig struct {
	h   *hub
	srv *httptest.Server
	t   *testing.T
}

func newRig(t *testing.T) *rig {
	db, e := openStore(filepath.Join(t.TempDir(), "radio.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	api, done, e := mediaAPI("127.0.0.1:0", "")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(done)
	h := newHub(db)
	h.api = api
	s := &server{h: h, limits: map[string]*rate.Limiter{}}
	srv := httptest.NewTLSServer(s.routes())
	t.Cleanup(srv.Close)
	stop := make(chan struct{})
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		tk := time.NewTicker(60 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-tk.C:
				h.tick()
			case <-stop:
				return
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-ended })
	return &rig{h, srv, t}
}

type peer struct {
	mu        sync.Mutex
	t         *testing.T
	id, token string
	ws        *websocket.Conn
	read      chan event
	failed    chan struct{}
	stash     []event
	tx        *webrtc.TrackLocalStaticRTP
	packets   chan []byte
	uplink    string
	seq       uint16
}

func (r *rig) enroll(dial, name string) (person, string) {
	t := r.t
	inv, e := r.h.db.invite(dial, name)
	if e != nil {
		t.Fatal(e)
	}
	res, e := r.srv.Client().Post(r.srv.URL+"/v1/login", "application/json", bytes.NewReader(encode(event{"dial": dial, "invite": inv, "password": randomID()})))
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	var a struct {
		User  person
		Token string
	}
	if e = json.NewDecoder(res.Body).Decode(&a); e != nil || a.Token == "" {
		t.Fatalf("login status %d", res.StatusCode)
	}
	return a.User, a.Token
}
func (r *rig) peer(dial, name string) *peer {
	t := r.t
	p, token := r.enroll(dial, name)
	pool := x509.NewCertPool()
	pool.AddCert(r.srv.Certificate())
	d := websocket.Dialer{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	ws, _, e := d.Dial("wss"+strings.TrimPrefix(r.srv.URL, "https")+"/v1/radio", http.Header{"Authorization": []string{"Bearer " + token}})
	if e != nil {
		t.Fatal(e)
	}
	b := &peer{t: t, id: p.ID, token: token, ws: ws, read: make(chan event, 256), failed: make(chan struct{}), packets: make(chan []byte, 256)}
	closing := make(chan struct{})
	go func() {
		defer close(b.failed)
		for {
			var e event
			if ws.ReadJSON(&e) != nil {
				return
			}
			select {
			case b.read <- e:
			case <-closing:
				return
			}
		}
	}()
	t.Cleanup(func() { close(closing); ws.Close(); <-b.failed })
	b.wait("snapshot", nil)
	return b
}
func (p *peer) send(e event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.t.Helper()
	if err := p.ws.WriteJSON(e); err != nil {
		p.t.Fatal(err)
	}
}
func (p *peer) wait(kind string, filter func(event) bool) event {
	p.t.Helper()
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	match := func(e event) bool { return e["type"] == kind && (filter == nil || filter(e)) }
	for i, e := range p.stash {
		if match(e) {
			p.stash = append(p.stash[:i], p.stash[i+1:]...)
			return e
		}
	}
	for {
		select {
		case e := <-p.read:
			if match(e) {
				return e
			}
			p.stash = append(p.stash, e)
		case <-timer.C:
			p.t.Fatalf("timed out waiting for %s", kind)
		case <-p.failed:
			p.t.Fatalf("socket closed waiting for %s", kind)
		}
	}
}
func (p *peer) path(role string) {
	p.t.Helper()
	var setting webrtc.SettingEngine
	setting.SetIncludeLoopbackCandidate(true)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(setting))
	pc, e := api.NewPeerConnection(webrtc.Configuration{})
	if e != nil {
		p.t.Fatal(e)
	}
	p.t.Cleanup(func() { pc.Close() })
	if role == "up" {
		tx, e := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "mic", "radio")
		if e != nil {
			p.t.Fatal(e)
		}
		_, e = pc.AddTransceiverFromTrack(tx, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionSendonly})
		if e != nil {
			p.t.Fatal(e)
		}
		p.tx = tx
	} else {
		_, e = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly})
		if e != nil {
			p.t.Fatal(e)
		}
		pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			for {
				pkt, _, e := t.ReadRTP()
				if e != nil {
					return
				}
				select {
				case p.packets <- append([]byte(nil), pkt.Payload...):
				default:
				}
			}
		})
	}
	offer, e := pc.CreateOffer(nil)
	if e != nil {
		p.t.Fatal(e)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if e = pc.SetLocalDescription(offer); e != nil {
		p.t.Fatal(e)
	}
	<-done
	id := randomID()
	p.send(event{"type": "offer", "role": role, "id": id, "sdp": pc.LocalDescription().SDP})
	ans := p.wait("answer", func(e event) bool { return e["id"] == id })
	if e = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: ans["sdp"].(string)}); e != nil {
		p.t.Fatal(e)
	}
	p.wait("path", func(e event) bool { return e["id"] == id })
	if role == "up" {
		p.uplink = id
	}
}
func (p *peer) on() {
	p.path("down")
	p.path("up")
	p.send(event{"type": "listen"})
	p.send(event{"type": "ping"})
	p.wait("pong", nil)
}
func (p *peer) press(to string, seq int) string {
	id := randomID()
	p.send(event{"type": "press", "to": to, "seq": seq, "request": id, "id": p.uplink})
	return id
}
func (p *peer) tone() []byte {
	payload, e := os.ReadFile("testdata/tone.opus-packet")
	if e != nil {
		p.t.Fatal(e)
	}
	for i := 0; i < 12; i++ {
		p.seq++
		e = p.tx.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: p.seq, Timestamp: uint32(p.seq) * 960}, Payload: payload})
		if e != nil {
			p.t.Fatal(e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return payload
}
func (p *peer) got(payload []byte) {
	p.t.Helper()
	select {
	case pkt := <-p.packets:
		if !bytes.Equal(payload, pkt) {
			p.t.Fatal("Opus payload changed")
		}
	case <-time.After(2 * time.Second):
		p.t.Fatal("no relayed Opus packet")
	}
}
func (p *peer) none() {
	p.t.Helper()
	select {
	case <-p.packets:
		p.t.Fatal("unexpected media")
	case <-time.After(100 * time.Millisecond):
	}
}
func approve(a, b *peer) {
	a.send(event{"type": "contact", "to": b.id, "action": "request"})
	a.send(event{"type": "ping"})
	a.wait("pong", nil)
	b.send(event{"type": "contact", "to": a.id, "action": "accept"})
	b.send(event{"type": "ping"})
	b.wait("pong", nil)
}
func exchange(from, to *peer, seq int) {
	from.t.Helper()
	req := from.press(to.id, seq)
	m := to.wait("incoming", nil)
	if m["from"] != from.id {
		from.t.Fatal("speaker identity")
	}
	to.send(event{"type": "ready", "burst": m["burst"]})
	from.wait("grant", func(e event) bool { return e["request"] == req })
	to.got(from.tone())
	from.send(event{"type": "release", "request": req})
	to.wait("free", func(e event) bool { return e["reply"] == true })
	from.wait("free", nil)
}
func TestEncryptedPrivateExchange(t *testing.T) {
	r := newRig(t)
	a := r.peer("01*12*1", "Alex")
	b := r.peer("01*12*2", "Alex")
	a.on()
	b.on()
	a.press(b.id, 1)
	if a.wait("denied", nil)["reason"] != "unavailable" {
		t.Fatal("consent required")
	}
	a.tone()
	b.none()
	approve(a, b)
	req := a.press(b.id, 2)
	m := b.wait("incoming", nil)
	a.send(event{"type": "release", "request": req})
	a.wait("free", nil)
	b.wait("free", nil)
	b.send(event{"type": "ready", "burst": m["burst"]})
	a.send(event{"type": "release", "request": req})
	a.tone()
	b.none()
	a.path("up")
	exchange(a, b, 3)
	exchange(b, a, 1)
}
func TestPrivateContention100(t *testing.T) {
	h, x, y := model(t)
	now := time.Now()
	h.now = func() time.Time { return now }
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := 1; i <= 100; i++ {
		for _, p := range []*handset{x, y} {
			p.listening = true
			p.down = &leg{ready: true}
			p.up = &leg{ready: true, id: "warm"}
		}
		now = now.Add(time.Second)
		// Start from a common barrier; both goroutines use the service's real serialized handler.
		h.mu.Unlock()
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, pp := range [][2]*handset{{x, y}, {y, x}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				h.handle(pp[0], command{Type: "press", To: pp[1].id, ID: "warm", Seq: int64(i), Request: randomID()})
			}()
		}
		close(start)
		wg.Wait()
		h.mu.Lock()
		c := x.call
		if c == nil || y.call != c || c.phase != "setup" {
			t.Fatal("reservation")
		}
		h.ready(c.listener, command{Burst: c.burst})
		if c.phase != "live" {
			t.Fatal("no grant")
		}
		h.end(c, "ended", false)
	}
	if h.grants != 100 || h.denials != 100 {
		t.Fatalf("grants=%d denials=%d", h.grants, h.denials)
	}
}
