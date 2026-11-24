// store.go
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

func randomID() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func digest(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }

var dialPattern = regexp.MustCompile(`^[0-9]{1,7}\*[0-9]{1,7}\*[0-9]{1,7}$`)

func validDial(s string) bool { return len(s) <= 15 && dialPattern.MatchString(s) }

type person struct {
	ID       string `json:"id"`
	Dial     string `json:"dial"`
	Name     string `json:"name"`
	Relation string `json:"relation,omitempty"`
	Presence string `json:"presence,omitempty"`
}
type store struct{ *sql.DB }

func openStore(path string) (*store, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return nil, err
	}
	if version > 1 {
		return nil, errors.New("database requires newer service")
	}
	_, err = db.Exec(`BEGIN;
 CREATE TABLE IF NOT EXISTS users(id TEXT PRIMARY KEY, dial TEXT UNIQUE NOT NULL, name TEXT NOT NULL, password BLOB, invite TEXT UNIQUE, expires INTEGER, disabled INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS tokens(hash TEXT PRIMARY KEY, user TEXT UNIQUE REFERENCES users(id), expires INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS contacts(a TEXT REFERENCES users(id), b TEXT REFERENCES users(id), requester TEXT REFERENCES users(id), approved INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(a,b), CHECK(a<b));
 CREATE TABLE IF NOT EXISTS audit(at INTEGER NOT NULL DEFAULT(unixepoch()), action TEXT NOT NULL, subject TEXT NOT NULL);
 PRAGMA user_version=1; COMMIT;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &store{db}, nil
}
func (s *store) invite(dial, name string) (string, error) {
	if !validDial(dial) || strings.TrimSpace(name) == "" || len(name) > 96 {
		return "", errors.New("invalid private ID or name")
	}
	id, code := randomID(), randomID()
	tx, e := s.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO users(id,dial,name,invite,expires) VALUES(?,?,?,?,?)", id, dial, name, digest(code), time.Now().Add(24*time.Hour).Unix()); e != nil {
		return "", e
	}
	if _, e = tx.Exec("INSERT INTO audit(action,subject) VALUES('invite',?)", id); e != nil {
		return "", e
	}
	return code, tx.Commit()
}
func (s *store) recover(dial string, disable bool) (string, error) {
	tx, e := s.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRow("SELECT id FROM users WHERE dial=?", dial).Scan(&id); e != nil {
		return "", e
	}
	action := "recover"
	if disable {
		action = "disable"
	}
	code := randomID()
	if _, e = tx.Exec("UPDATE users SET password=NULL,invite=?,expires=?,disabled=? WHERE id=?", digest(code), time.Now().Add(24*time.Hour).Unix(), disable, id); e != nil {
		return "", e
	}
	if _, e = tx.Exec("DELETE FROM tokens WHERE user=?", id); e != nil {
		return "", e
	}
	if _, e = tx.Exec("INSERT INTO audit(action,subject) VALUES(?,?)", action, id); e != nil {
		return "", e
	}
	return code, tx.Commit()
}
func (s *store) login(dial, password, invite string) (person, string, error) {
	var p person
	var hash []byte
	if !validDial(dial) || len(password) < 10 || len(password) > 72 {
		return p, "", errors.New("credentials")
	}
	var newHash []byte
	var e error
	if invite != "" {
		newHash, e = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if e != nil {
			return p, "", e
		}
	}
	tx, e := s.Begin()
	if e != nil {
		return p, "", e
	}
	defer tx.Rollback()
	e = tx.QueryRow("SELECT id,dial,name,password FROM users WHERE dial=? AND disabled=0", dial).Scan(&p.ID, &p.Dial, &p.Name, &hash)
	if e != nil {
		return p, "", e
	}
	if invite != "" {
		r, e := tx.Exec("UPDATE users SET password=?,invite=NULL,expires=NULL WHERE id=? AND invite=? AND expires>unixepoch()", newHash, p.ID, digest(invite))
		if e != nil {
			return p, "", e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return p, "", errors.New("invite")
		}
	} else if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil {
		return p, "", errors.New("credentials")
	}
	token := randomID()
	_, e = tx.Exec("INSERT OR REPLACE INTO tokens(hash,user,expires) VALUES(?,?,?)", digest(token), p.ID, time.Now().Add(12*time.Hour).Unix())
	if e != nil {
		return p, "", e
	}
	return p, token, tx.Commit()
}
func (s *store) auth(hash string) (string, error) {
	var id string
	e := s.QueryRow("SELECT user FROM tokens JOIN users ON users.id=tokens.user WHERE hash=? AND tokens.expires>unixepoch() AND disabled=0", hash).Scan(&id)
	return id, e
}
func pair(a, b string) (string, string) {
	if a > b {
		return b, a
	}
	return a, b
}
func (s *store) approved(a, b string) bool {
	a, b = pair(a, b)
	var x int
	return s.QueryRow("SELECT approved FROM contacts WHERE a=? AND b=?", a, b).Scan(&x) == nil && x == 1
}
func (s *store) contact(from, to, action string) error {
	if from == to {
		return errors.New("self")
	}
	a, b := pair(from, to)
	switch action {
	case "request":
		_, e := s.Exec("INSERT OR IGNORE INTO contacts(a,b,requester) SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM users WHERE id=? AND disabled=0 AND password IS NOT NULL)", a, b, from, to)
		return e
	case "accept":
		_, e := s.Exec("UPDATE contacts SET approved=1 WHERE a=? AND b=? AND requester<>?", a, b, from)
		return e
	case "remove":
		_, e := s.Exec("DELETE FROM contacts WHERE a=? AND b=?", a, b)
		return e
	}
	return fmt.Errorf("unknown contact action")
}
func (s *store) directory(user string) ([]person, error) {
	rows, e := s.Query(`SELECT u.id,u.dial,u.name,CASE WHEN c.approved=1 THEN 'approved' WHEN c.requester=? THEN 'sent' WHEN c.requester IS NOT NULL THEN 'received' ELSE 'none' END FROM users u LEFT JOIN contacts c ON c.a=min(?,u.id) AND c.b=max(?,u.id) WHERE u.id<>? AND u.disabled=0 AND u.password IS NOT NULL ORDER BY u.name,u.dial`, user, user, user, user)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []person{}
	for rows.Next() {
		var p person
		if e = rows.Scan(&p.ID, &p.Dial, &p.Name, &p.Relation); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
