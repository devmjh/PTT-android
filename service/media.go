// media.go
package main

import (
	"crypto/hmac"
	"crypto/sha1" // TURN REST long-term credential derivation used by coturn.
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

func mediaAPI(bind, advertise string) (*webrtc.API, func(), error) {
	var cfg webrtc.SettingEngine
	cfg.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	cfg.SetICETimeouts(4*time.Second, 12*time.Second, time.Second)
	addr, e := net.ResolveUDPAddr("udp4", bind)
	if e != nil {
		return nil, nil, e
	}
	conn, e := net.ListenUDP("udp4", addr)
	if e != nil {
		return nil, nil, e
	}
	mux := ice.NewUDPMuxDefault(ice.UDPMuxParams{UDPConn: conn})
	cfg.SetICEUDPMux(mux)
	if advertise != "" {
		cfg.SetNAT1To1IPs([]string{advertise}, webrtc.ICECandidateTypeHost)
	}
	if addr.IP.IsLoopback() {
		cfg.SetIncludeLoopbackCandidate(true)
	}
	m := &webrtc.MediaEngine{}
	e = m.RegisterCodec(webrtc.RTPCodecParameters{RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}, PayloadType: 111}, webrtc.RTPCodecTypeAudio)
	if e != nil {
		mux.Close()
		return nil, nil, e
	}
	ir := &interceptor.Registry{}
	if e = webrtc.RegisterDefaultInterceptors(m, ir); e != nil {
		mux.Close()
		return nil, nil, e
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(cfg), webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(ir)), func() { mux.Close() }, nil
}
func (h *hub) iceConfig(p *handset) []event {
	out := []event{}
	for _, url := range h.ice {
		entry := event{"url": url}
		if strings.HasPrefix(url, "turn:") || strings.HasPrefix(url, "turns:") {
			user := fmt.Sprintf("%d:%s", time.Now().Add(time.Hour).Unix(), p.session)
			mac := hmac.New(sha1.New, []byte(h.turnSecret))
			mac.Write([]byte(user))
			entry["username"] = user
			entry["password"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
		}
		out = append(out, entry)
	}
	return out
}
func (p *handset) slot(role string) **leg {
	if role == "up" {
		return &p.up
	}
	return &p.down
}

// Each uplink DTLS association can carry exactly one granted burst. Closing it
// on release prevents old SRTP, including in-flight packets, entering a new grant.
func (h *hub) offer(p *handset, m command) error {
	if m.Role != "up" && m.Role != "down" || m.ID == "" || len(m.ID) > 96 || len(m.SDP) > 32768 {
		return fmt.Errorf("offer")
	}
	h.mu.Lock()
	if !h.current(p) || *p.slot(m.Role) != nil {
		h.mu.Unlock()
		return fmt.Errorf("duplicate leg")
	}
	pc, e := h.api.NewPeerConnection(webrtc.Configuration{})
	if e != nil {
		h.mu.Unlock()
		return e
	}
	l := &leg{id: m.ID, pc: pc}
	*p.slot(m.Role) = l
	h.mu.Unlock()
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if !h.current(p) || *p.slot(m.Role) != l {
			return
		}
		switch st {
		case webrtc.PeerConnectionStateConnected:
			l.ready = true
			p.send(event{"type": "path", "role": m.Role, "id": l.id})
			h.changed()
		case webrtc.PeerConnectionStateDisconnected, webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			h.remove(p, "media")
		}
	})
	if m.Role == "up" {
		pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			if t.Kind() != webrtc.RTPCodecTypeAudio || !strings.EqualFold(t.Codec().MimeType, webrtc.MimeTypeOpus) {
				return
			}
			for {
				pkt, _, e := t.ReadRTP()
				if e != nil {
					return
				}
				h.forward(p, l, pkt)
			}
		})
	} else {
		track, e := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "voice", "radio")
		if e != nil {
			return e
		}
		l.track = track
		sender, e := pc.AddTrack(track)
		if e != nil {
			return e
		}
		go func() {
			buf := make([]byte, 1500)
			for {
				if _, _, e := sender.Read(buf); e != nil {
					return
				}
			}
		}()
	}
	if e = pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: m.SDP}); e != nil {
		return e
	}
	ts := pc.GetTransceivers()
	if len(ts) != 1 || ts[0].Kind() != webrtc.RTPCodecTypeAudio {
		return fmt.Errorf("single audio m-line required")
	}
	answer, e := pc.CreateAnswer(nil)
	if e != nil {
		return e
	}
	done := webrtc.GatheringCompletePromise(pc)
	if e = pc.SetLocalDescription(answer); e != nil {
		return e
	}
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		return fmt.Errorf("ICE gather")
	}
	p.send(event{"type": "answer", "role": m.Role, "id": l.id, "sdp": pc.LocalDescription().SDP})
	return nil
}
func (h *hub) candidate(p *handset, m command) {
	h.mu.Lock()
	l := *p.slot(m.Role)
	h.mu.Unlock()
	if l != nil && l.id == m.ID {
		_ = l.pc.AddICECandidate(m.Candidate)
	}
}
