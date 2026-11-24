// server.go
package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type server struct {
	h      *hub
	mu     sync.Mutex
	limits map[string]*rate.Limiter
}

func (s *server) allow(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.limits) > 4096 {
		for k, v := range s.limits {
			if v.Tokens() >= float64(v.Burst()) {
				delete(s.limits, k)
			}
		}
	}
	l := s.limits[key]
	if l == nil {
		l = rate.NewLimiter(rate.Every(3*time.Second), 8)
		s.limits[key] = l
	}
	return l.Allow()
}
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (s *server) token(r *http.Request) (string, string) {
	v := r.Header.Get("Authorization")
	if !strings.HasPrefix(v, "Bearer ") {
		return "", ""
	}
	hash := digest(strings.TrimPrefix(v, "Bearer "))
	id, _ := s.h.db.auth(hash)
	return id, hash
}
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if s.h.db.Ping() != nil {
			http.Error(w, "database", 503)
			return
		}
		s.h.mu.Lock()
		counts := event{"status": "ready", "protocol": 1, "connected": len(s.h.peers), "grants": s.h.grants, "denials": s.h.denials, "forwardedOpusPackets": s.h.packets}
		s.h.mu.Unlock()
		reply(w, counts)
	})
	mux.HandleFunc("POST /v1/login", func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !s.allow(ip) {
			http.Error(w, "slow_down", 429)
			return
		}
		var a struct{ Dial, Password, Invite string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&a) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		p, token, e := s.h.db.login(a.Dial, a.Password, a.Invite)
		if e != nil {
			http.Error(w, "credentials", 401)
			return
		}
		s.h.mu.Lock()
		if old := s.h.peers[p.ID]; old != nil && old.hash != digest(token) {
			s.h.remove(old, "signed_out")
		}
		s.h.changed()
		s.h.mu.Unlock()
		reply(w, event{"user": p, "token": token})
	})
	mux.HandleFunc("POST /v1/logout", func(w http.ResponseWriter, r *http.Request) {
		id, hash := s.token(r)
		if id == "" {
			http.Error(w, "credentials", 401)
			return
		}
		s.h.mu.Lock()
		defer s.h.mu.Unlock()
		if _, e := s.h.db.Exec("DELETE FROM tokens WHERE hash=?", hash); e != nil {
			http.Error(w, "database", 500)
			return
		}
		if p := s.h.peers[id]; p != nil && p.hash == hash {
			s.h.remove(p, "signed_out")
		}
		reply(w, event{"ok": true})
	})
	mux.HandleFunc("GET /v1/radio", s.radio)
	return mux
}
func (s *server) radio(w http.ResponseWriter, r *http.Request) {
	id, hash := s.token(r)
	if id == "" {
		http.Error(w, "credentials", 401)
		return
	}
	u := websocket.Upgrader{HandshakeTimeout: 5 * time.Second}
	ws, e := u.Upgrade(w, r, nil)
	if e != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	p := &handset{id: id, hash: hash, session: randomID(), out: make(chan event, 128), cancel: cancel, lastSeen: time.Now()}
	s.h.mu.Lock()
	if current, _ := s.h.db.auth(hash); current != id {
		s.h.mu.Unlock()
		return
	}
	if old := s.h.peers[id]; old != nil {
		s.h.remove(old, "replaced")
	}
	s.h.peers[id] = p
	s.h.snapshot(p)
	s.h.mu.Unlock()
	defer func() { s.h.mu.Lock(); defer s.h.mu.Unlock(); s.h.remove(p, "offline") }()
	go func() {
		defer ws.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-p.out:
				ws.SetWriteDeadline(time.Now().Add(4 * time.Second))
				if ws.WriteJSON(msg) != nil {
					return
				}
			}
		}
	}()
	ws.SetReadLimit(49152)
	lim := rate.NewLimiter(35, 80)
	for {
		var m command
		if e = ws.ReadJSON(&m); e != nil || !lim.Allow() {
			return
		}
		switch m.Type {
		case "offer":
			if s.h.offer(p, m) != nil {
				return
			}
		case "ice":
			s.h.candidate(p, m)
		default:
			s.h.handle(p, m)
		}
	}
}
