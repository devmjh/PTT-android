// handset_test.go
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The USB tunnel carries TLS signaling and TURN/TCP. Only the two loopback
// test listeners are exposed to the handset. They close when the test ends.
func TestUSBHandset(t *testing.T) {
	if os.Getenv("RIDGE_HANDSET") != "1" {
		t.Skip("requires an unlocked handset, adb, and both installed APKs")
	}
	r := newRig(t)
	ctx, stop := context.WithTimeout(context.Background(), 95*time.Second)
	defer stop()
	adb := func(args ...string) {
		t.Helper()
		out, e := exec.CommandContext(ctx, "adb", args...).CombinedOutput()
		if e != nil {
			t.Fatalf("adb: %s", out)
		}
	}
	reverse := func(addr string) {
		_, port, e := net.SplitHostPort(addr)
		if e != nil {
			t.Fatal(e)
		}
		spec := "tcp:" + port
		adb("reverse", "--no-rebind", spec, spec)
		t.Cleanup(func() { exec.Command("adb", "reverse", "--remove", spec).Run() })
	}
	u, _ := url.Parse(r.srv.URL)
	reverse(u.Host)
	b := r.peer("01*12*2", "Bench peer")
	b.on()
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	secret := randomID()
	turnAddress := ln.Addr().String()
	ln.Close()
	control, e := startTURN(turnAddress, "127.0.0.1", secret)
	if e != nil {
		t.Fatal(e)
	}
	defer control.Close()
	reverse(turnAddress)
	r.h.mu.Lock()
	r.h.turnSecret = secret
	r.h.ice = []string{"turn:" + turnAddress + "?transport=tcp"}
	r.h.relayOnly = true
	r.h.mu.Unlock()
	code, e := r.h.db.invite("01*12*1", "Lab handset")
	if e != nil {
		t.Fatal(e)
	}
	var phone string
	if e = r.h.db.QueryRow("SELECT id FROM users WHERE dial='01*12*1'").Scan(&phone); e != nil {
		t.Fatal(e)
	}
	cfg := event{"host": r.srv.URL, "cert": base64.StdEncoding.EncodeToString(r.srv.Certificate().Raw), "peer": b.id, "invite": code, "password": randomID()}
	prep := exec.CommandContext(ctx, "adb", "shell", "-T", "run-as lab.ridgeradio sh -c 'umask 077; cat > cache/ridge-bench.json'")
	prep.Stdin = bytes.NewReader(encode(cfg))
	if out, e := prep.CombinedOutput(); e != nil {
		t.Fatalf("bench setup: %s", out)
	}
	t.Cleanup(func() {
		exec.Command("adb", "shell", "run-as", "lab.ridgeradio", "rm", "-f", "cache/ridge-bench.json").Run()
	})
	adb("shell", "pm", "grant", "lab.ridgeradio", "android.permission.RECORD_AUDIO")
	run := exec.CommandContext(ctx, "adb", "shell", "am", "instrument", "-w", "-r", "-e", "class", "lab.ridgeradio.HandsetTest#privateTurnExchange", "lab.ridgeradio.test/androidx.test.runner.AndroidJUnitRunner")
	var output bytes.Buffer
	run.Stdout = &output
	run.Stderr = &output
	if e = run.Start(); e != nil {
		t.Fatal(e)
	}
	ended := make(chan struct{})
	go func() { _ = run.Wait(); close(ended) }()
	defer func() {
		if t.Failed() {
			select {
			case <-ended:
			case <-time.After(12 * time.Second):
				stop()
				<-ended
			}
			t.Log(output.String())
		}
	}()
	// Keep the headless radio's presence alive while Android starts and negotiates.
	// Only this goroutine writes after this point, via the peer's serialized writer.
	heart, finish := context.WithCancel(ctx)
	defer finish()
	go func() {
		tk := time.NewTicker(5 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-heart.Done():
				return
			case <-tk.C:
				b.mu.Lock()
				_ = b.ws.WriteJSON(event{"type": "ping"})
				b.mu.Unlock()
			}
		}
	}()
	b.wait("snapshot", func(e event) bool {
		for _, v := range e["people"].([]any) {
			p := v.(map[string]any)
			if p["id"] == phone && p["relation"] == "received" {
				return true
			}
		}
		return false
	})
	b.send(event{"type": "contact", "to": phone, "action": "accept"})
	m := b.wait("incoming", nil)
	b.send(event{"type": "ready", "burst": m["burst"]})
	free := b.wait("free", nil)
	if free["reply"] != true {
		t.Fatal("phone did not complete a burst")
	}
	packets := len(b.packets)
	if packets < 3 {
		t.Fatalf("received %d handset Opus packets", packets)
	}
	// Discard the handset audio, retaining only its count.
	for len(b.packets) > 0 {
		<-b.packets
	}
	req := b.press(phone, 1)
	b.wait("grant", func(e event) bool { return e["request"] == req })
	b.tone()
	b.send(event{"type": "release", "request": req})
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("instrumentation deadline")
	}
	if !strings.Contains(output.String(), "OK (1 test)") {
		t.Fatal("Android instrumentation failed")
	}
	fmt.Printf("Samsung-side instrumentation passed. Software peer received %d handset Opus packets; handset asserted >=3 packets on reply. TURN/TCP over USB.\n", packets)
}
