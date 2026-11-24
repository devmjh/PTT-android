// floor.go
package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type event map[string]any
type command struct {
	Type      string                  `json:"type"`
	To        string                  `json:"to"`
	ID        string                  `json:"id"`
	Request   string                  `json:"request"`
	Burst     string                  `json:"burst"`
	Seq       int64                   `json:"seq"`
	Role      string                  `json:"role"`
	SDP       string                  `json:"sdp"`
	Candidate webrtc.ICECandidateInit `json:"candidate"`
	Action    string                  `json:"action"`
}
type leg struct {
	id    string
	pc    *webrtc.PeerConnection
	ready bool
	track *webrtc.TrackLocalStaticRTP
}
type handset struct {
	id, hash, session   string
	out                 chan event
	cancel              context.CancelFunc
	up, down            *leg
	listening           bool
	lastSeen, lastPress time.Time
	seq                 int64
	call                *call
	// Keep one receiving RTP timeline even when subsequent speakers have independent clocks.
	seqOut   uint16
	stampOut uint32
}

func (h *handset) send(e event) {
	select {
	case h.out <- e:
	default:
		h.cancel()
	}
}

type call struct {
	talker, listener *handset
	request, burst   string
	upload           *leg
	phase            string
	deadline, cap    time.Time
	warned           bool
	hasPacket        bool
	inputSeq         uint16
	inputTime        uint32
}
type hub struct {
	mu                       sync.Mutex
	peers                    map[string]*handset
	db                       *store
	api                      *webrtc.API
	ice                      []string
	turnSecret               string
	relayOnly                bool
	now                      func() time.Time
	grants, denials, packets uint64
}

func newHub(db *store) *hub            { return &hub{peers: map[string]*handset{}, db: db, now: time.Now} }
func (h *hub) current(p *handset) bool { return h.peers[p.id] == p }
func (h *hub) presence(id string) string {
	p := h.peers[id]
	if p == nil || !p.listening || p.down == nil || !p.down.ready {
		return "Unavailable"
	}
	if p.call != nil {
		return "Busy"
	}
	return "Ready"
}
func (h *hub) snapshot(p *handset) {
	people, e := h.db.directory(p.id)
	if e != nil {
		p.cancel()
		return
	}
	for i := range people {
		people[i].Presence = "Unknown"
		if people[i].Relation == "approved" {
			people[i].Presence = h.presence(people[i].ID)
		}
	}
	p.send(event{"type": "snapshot", "session": p.session, "people": people, "ice": h.iceConfig(p), "relayOnly": h.relayOnly})
}
func (h *hub) changed() {
	for _, p := range h.peers {
		h.snapshot(p)
	}
}
func (h *hub) remove(p *handset, reason string) {
	if !h.current(p) {
		return
	}
	delete(h.peers, p.id)
	if p.call != nil {
		h.end(p.call, reason, false)
	}
	for _, l := range []*leg{p.up, p.down} {
		if l != nil && l.pc != nil {
			go l.pc.Close()
		}
	}
	p.cancel()
	h.changed()
}
func (h *hub) deny(p *handset, m command, why string) {
	h.denials++
	p.send(event{"type": "denied", "request": m.Request, "reason": why})
}
func (h *hub) press(p *handset, m command) {
	now := h.now()
	if m.Seq <= p.seq || m.Request == "" || len(m.Request) > 100 {
		h.deny(p, m, "stale")
		return
	}
	p.seq = m.Seq
	if now.Sub(p.lastPress) < 120*time.Millisecond {
		h.deny(p, m, "slow_down")
		return
	}
	p.lastPress = now
	q := h.peers[m.To]
	if q == nil || p == q || !h.db.approved(p.id, m.To) {
		h.deny(p, m, "unavailable")
		return
	}
	if !p.listening || p.up == nil || !p.up.ready || p.up.id != m.ID {
		h.deny(p, m, "media")
		return
	}
	if !q.listening || q.down == nil || !q.down.ready {
		h.deny(p, m, "unavailable")
		return
	}
	for _, party := range []*handset{p, q} {
		if c := party.call; c != nil && now.After(c.deadline) {
			h.expire(c)
		}
	}
	if p.call != nil || q.call != nil {
		c := p.call
		if c == nil || c != q.call || c.phase != "reply" {
			h.deny(p, m, "busy")
			return
		}
	}
	c := &call{talker: p, listener: q, request: m.Request, burst: randomID(), upload: p.up, phase: "setup", deadline: now.Add(3 * time.Second)}
	p.call = c
	q.call = c
	q.send(event{"type": "incoming", "from": p.id, "burst": c.burst})
}
func (h *hub) ready(p *handset, m command) {
	c := p.call
	if c == nil || c.listener != p || c.burst != m.Burst || c.phase != "setup" {
		return
	}
	now := h.now()
	if !now.Before(c.deadline) {
		h.expire(c)
		return
	}
	if !p.listening || p.down == nil || !p.down.ready || !c.upload.ready {
		h.end(c, "media", false)
		return
	}
	c.phase = "live"
	c.cap = now.Add(time.Minute)
	c.deadline = now.Add(2800 * time.Millisecond)
	h.grants++
	c.talker.send(event{"type": "grant", "request": c.request, "burst": c.burst, "id": c.upload.id})
}
func (h *hub) end(c *call, reason string, reply bool) {
	if c.talker.call != c {
		return
	}
	if c.phase != "reply" {
		if l := c.upload; c.talker.up == l && l != nil {
			c.talker.up = nil
			if l.pc != nil {
				go l.pc.Close()
			}
		}
	}
	for _, p := range []*handset{c.talker, c.listener} {
		p.send(event{"type": "free", "burst": c.burst, "request": c.request, "reason": reason, "reply": reply, "peer": other(c, p).id, "rotate": p == c.talker && c.phase != "reply"})
	}
	if reply {
		c.phase = "reply"
		c.deadline = h.now().Add(6 * time.Second)
	} else {
		c.talker.call = nil
		c.listener.call = nil
	}
}
func other(c *call, p *handset) *handset {
	if p == c.talker {
		return c.listener
	}
	return c.talker
}
func (h *hub) expire(c *call) {
	switch c.phase {
	case "setup":
		h.end(c, "timeout", false)
	case "live":
		reason := "lease"
		if !h.now().Before(c.cap) {
			reason = "limit"
		}
		h.end(c, reason, true)
	default:
		h.end(c, "ended", false)
	}
}
func (h *hub) tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for _, p := range h.peers {
		if now.Sub(p.lastSeen) > 22*time.Second {
			h.remove(p, "offline")
			continue
		}
		if id, e := h.db.auth(p.hash); e != nil || id != p.id {
			h.remove(p, "signed_out")
			continue
		}
		c := p.call
		if c == nil || c.talker != p {
			continue
		}
		if !now.Before(c.deadline) || c.phase == "live" && !now.Before(c.cap) {
			h.expire(c)
		} else if c.phase == "live" && !c.warned && now.Sub(c.cap) >= -5*time.Second {
			c.warned = true
			p.send(event{"type": "warning", "burst": c.burst})
		}
	}
}
func (h *hub) handle(p *handset, m command) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.current(p) {
		return
	}
	p.lastSeen = h.now()
	c := p.call
	switch m.Type {
	case "ping":
		p.send(event{"type": "pong"})
	case "listen":
		p.listening = true
		h.changed()
	case "contact":
		if h.db.contact(p.id, m.To, m.Action) == nil {
			if m.Action == "remove" && c != nil && other(c, p).id == m.To {
				h.end(c, "unavailable", false)
			}
			h.changed()
		}
	case "press":
		h.press(p, m)
	case "ready":
		h.ready(p, m)
	case "renew":
		if c != nil && c.phase == "live" && c.talker == p && c.burst == m.Burst && h.now().Before(c.deadline) && h.now().Before(c.cap) {
			c.deadline = minTime(h.now().Add(2800*time.Millisecond), c.cap)
			p.send(event{"type": "renewed", "burst": c.burst})
		}
	case "release":
		if c != nil && c.talker == p && c.request == m.Request && c.phase != "reply" {
			h.end(c, "released", c.phase == "live")
		}
	case "end":
		if c != nil {
			h.end(c, "ended", false)
		}
	case "cant_play":
		if c != nil && c.listener == p && c.burst == m.Burst {
			h.end(c, "route", false)
		}
	}
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (h *hub) forward(p *handset, l *leg, pkt *rtp.Packet) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := p.call
	if !h.current(p) || c == nil || c.talker != p || c.upload != l || c.phase != "live" || !h.now().Before(c.deadline) || !h.now().Before(c.cap) {
		return
	}
	q := c.listener
	if !q.listening || q.down == nil || !q.down.ready {
		return
	}
	ds := uint16(1)
	dt := uint32(960)
	if c.hasPacket {
		ds = pkt.SequenceNumber - c.inputSeq
		if ds == 0 || ds >= 32768 {
			return
		}
		dt = pkt.Timestamp - c.inputTime
	}
	c.inputSeq = pkt.SequenceNumber
	c.inputTime = pkt.Timestamp
	q.seqOut += ds
	q.stampOut += dt
	out := &rtp.Packet{Header: rtp.Header{Version: 2, Marker: !c.hasPacket || pkt.Marker, SequenceNumber: q.seqOut, Timestamp: q.stampOut}, Payload: pkt.Payload}
	c.hasPacket = true
	if err := q.down.track.WriteRTP(out); err != nil {
		h.end(c, "media", false)
	} else {
		h.packets++
	}
}
func encode(v any) []byte { b, _ := json.Marshal(v); return b }
