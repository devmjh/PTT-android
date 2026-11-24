// model_test.go
package main

import (
	"path/filepath"
	"testing"
	"time"
)

func model(t *testing.T) (*hub, *handset, *handset) {
	db, e := openStore(filepath.Join(t.TempDir(), "model.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	h := newHub(db)
	mk := func(d string) *handset {
		code, e := db.invite(d, "Same name")
		if e != nil {
			t.Fatal(e)
		}
		u, token, e := db.login(d, randomID(), code)
		if e != nil {
			t.Fatal(e)
		}
		p := &handset{id: u.ID, hash: digest(token), out: make(chan event, 1024), cancel: func() {}, listening: true, up: &leg{id: "warm", ready: true}, down: &leg{ready: true}}
		h.peers[p.id] = p
		return p
	}
	a, b := mk("1*1*1"), mk("1*1*2")
	if db.contact(a.id, b.id, "request") != nil || db.contact(b.id, a.id, "accept") != nil {
		t.Fatal("approval")
	}
	return h, a, b
}
func TestDeadlinesAndReply(t *testing.T) {
	h, a, b := model(t)
	now := time.Now()
	h.now = func() time.Time { return now }
	tick := func(d time.Duration) { now = now.Add(d); a.lastSeen = now; b.lastSeen = now; h.tick() }
	press := func(seq int64) {
		a.up = &leg{ready: true, id: "warm"}
		h.press(a, command{ID: "warm", Request: randomID(), To: b.id, Seq: seq})
	}
	press(1)
	tick(3 * time.Second)
	if a.call != nil {
		t.Fatal("setup still reserved")
	}
	press(2)
	h.ready(b, command{Burst: a.call.burst})
	tick(2801 * time.Millisecond)
	if a.call.phase != "reply" {
		t.Fatal("lost renewal did not close burst")
	}
	tick(6 * time.Second)
	if b.call != nil {
		t.Fatal("reply reservation not freed")
	}
	press(3)
	h.ready(b, command{Burst: a.call.burst})
	id := a.call.burst
	for i := 0; i < 60; i++ {
		tick(time.Second)
		h.handle(a, command{Type: "renew", Burst: id})
	}
	if a.call.phase != "reply" || !a.call.warned {
		t.Fatal("cap or warning")
	}
	h.handle(b, command{Type: "end"})
	if a.call != nil || b.call != nil {
		t.Fatal("end did not free peers")
	}
}
func TestDirectoryIdentityAndRecovery(t *testing.T) {
	for _, s := range []string{"164*123*4567", "00*0*0000001"} {
		if !validDial(s) {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"1**2", "1* 1*2", "12345678*1*1", "1234567*123456*12", "１*1*1"} {
		if validDial(s) {
			t.Fatal(s)
		}
	}
	h, a, b := model(t)
	if _, e := h.db.invite("1*1*1", "Duplicate"); e == nil {
		t.Fatal("duplicate")
	}
	code, e := h.db.recover("1*1*1", false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = h.db.auth(a.hash); e == nil {
		t.Fatal("prior token active")
	}
	pw := randomID()
	u, first, e := h.db.login("1*1*1", pw, code)
	if e != nil || u.ID != a.id || !h.db.approved(a.id, b.id) {
		t.Fatal("identity not restored")
	}
	if _, _, e = h.db.login("1*1*1", pw, code); e == nil {
		t.Fatal("reused invite")
	}
	if _, _, e = h.db.login("1*1*1", pw, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = h.db.auth(digest(first)); e == nil {
		t.Fatal("replacement left old token")
	}
}
func TestBackupRestore(t *testing.T) {
	h, a, b := model(t)
	path := filepath.Join(t.TempDir(), "restored.db")
	if _, e := h.db.Exec("VACUUM INTO ?", path); e != nil {
		t.Fatal(e)
	}
	restored, e := openStore(path)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	people, e := restored.directory(a.id)
	if e != nil || len(people) != 1 || people[0].ID != b.id || people[0].Relation != "approved" {
		t.Fatal("restore mismatch")
	}
}
