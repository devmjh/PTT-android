// main.go
package main

import (
	"crypto/tls"
	"fmt"
	"golang.org/x/time/rate"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func main() {
	syscall.Umask(0077)
	if len(os.Args) < 2 {
		log.Fatal("usage: ridge serve | invite DIAL NAME | recover DIAL | disable DIAL | backup NEW_FILE")
	}
	path := env("RIDGE_DB", "ridge.db")
	db, e := openStore(path)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	switch os.Args[1] {
	case "invite":
		if len(os.Args) != 4 {
			log.Fatal("invite DIAL NAME")
		}
		code, e := db.invite(os.Args[2], os.Args[3])
		if e != nil {
			log.Fatal(e)
		}
		fmt.Println(code)
	case "disable", "recover":
		if len(os.Args) != 3 {
			log.Fatal("requires private ID")
		}
		disable := os.Args[1] == "disable"
		code, e := db.recover(os.Args[2], disable)
		if e != nil {
			log.Fatal(e)
		}
		if !disable {
			fmt.Println(code)
		}
	case "backup":
		if len(os.Args) != 3 {
			log.Fatal("backup NEW_FILE")
		}
		f, e := os.OpenFile(os.Args[2], os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			log.Fatal(e)
		}
		f.Close()
		if _, e = db.Exec("VACUUM INTO ?", os.Args[2]); e != nil {
			log.Fatal(e)
		}
	case "serve":
		f, e := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			log.Fatal(e)
		}
		defer f.Close()
		if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			log.Fatal("this database already has a floor authority")
		}
		h := newHub(db)
		api, closeMedia, e := mediaAPI(env("RIDGE_MEDIA_BIND", "0.0.0.0:40000"), os.Getenv("RIDGE_PUBLIC_IP"))
		if e != nil {
			log.Fatal(e)
		}
		defer closeMedia()
		h.api = api
		h.relayOnly = os.Getenv("RIDGE_RELAY_ONLY") == "1"
		h.turnSecret = os.Getenv("RIDGE_TURN_SECRET")
		if bind := os.Getenv("RIDGE_TURN_BIND"); bind != "" {
			turnServer, err := startTURN(bind, os.Getenv("RIDGE_PUBLIC_IP"), h.turnSecret)
			if err != nil {
				log.Fatal(err)
			}
			defer turnServer.Close()
		}
		for _, v := range strings.Split(os.Getenv("RIDGE_ICE_URLS"), ",") {
			v = strings.TrimSpace(v)
			if v != "" {
				if strings.HasPrefix(v, "turn") && h.turnSecret == "" {
					log.Fatal("TURN secret required")
				}
				h.ice = append(h.ice, v)
			}
		}
		s := &server{h: h, limits: map[string]*rate.Limiter{}}
		srv := &http.Server{Addr: env("RIDGE_BIND", "127.0.0.1:8443"), Handler: s.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
		go func() {
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			for range t.C {
				h.tick()
			}
		}()
		cert, key := os.Getenv("RIDGE_CERT"), os.Getenv("RIDGE_KEY")
		if cert != "" && key != "" {
			log.Fatal(srv.ListenAndServeTLS(cert, key))
		}
		host, _, e := net.SplitHostPort(srv.Addr)
		if e != nil || !net.ParseIP(host).IsLoopback() {
			log.Fatal("direct listening requires RIDGE_CERT and RIDGE_KEY; reverse proxy HTTP must bind to loopback")
		}
		log.Fatal(srv.ListenAndServe())
	default:
		log.Fatal("unknown command")
	}
}
