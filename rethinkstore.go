// Copyright 2015 Brian "bojo" Jones. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package rethinkstore

import (
	"bytes"
	"encoding/base32"
	"encoding/gob"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	r "gopkg.in/rethinkdb/rethinkdb-go.v5"
)

var ErrNoDatabase = errors.New("no databases available")

// Amount of time for keys to expire.
var sessionExpire = 86400 * 30

type RethinkSession struct {
	Id      string    `rethinkdb:"id"`
	Expires time.Time `rethinkdb:"expires"`
	Session []byte    `rethinkdb:"session"`
}

// RethinkStore stores sessions in a rethinkdb backend.
type RethinkStore struct {
	Rethink       *r.Session           // rethink session
	Table         string               // table to store sessions in
	Codecs        []securecookie.Codec // session codecs
	Options       *sessions.Options    // default configuration
	DefaultMaxAge int                  // default TTL for a MaxAge == 0 session
}

// NewRethinkStore returns a new RethinkStore.
//
// Takes in the database address, database name, session table,
// max idle connections, max open connections, and session key pairs.
func NewRethinkStore(addr, db, table string, idle, open int, keyPairs ...[]byte) (*RethinkStore, error) {
	session, err := r.Connect(r.ConnectOpts{
		Address:  addr,
		Database: db,
		MaxIdle:  idle,
		MaxOpen:  open,
	})
	if err != nil {
		return nil, err
	}
	rs := &RethinkStore{
		Rethink: session,
		Table:   table,
		Codecs:  securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: sessionExpire,
		},
	}

	rs.MaxAge(sessionExpire)

	// Create missing db, table and secondary index. Discard error (database exists)
	r.DBCreate(db).RunWrite(session)
	r.DB(db).TableCreate(table).RunWrite(session)

	// Index for removing expired data
	r.Table(table).IndexCreate("expires").Exec(session)
	r.Table(table).IndexWait().RunWrite(session)

	return rs, nil
}

// Close closes the underlying Rethink Client.
func (s *RethinkStore) Close() {
	s.Rethink.Close()
}

// Get returns a session for the given name after adding it to the registry.
func (s *RethinkStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New returns a session for the given name without adding it to the registry.
func (s *RethinkStore) New(r *http.Request, name string) (*sessions.Session, error) {
	var err error
	session := sessions.NewSession(s, name)
	session.Options = &(*s.Options)
	session.IsNew = true
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, s.Codecs...)
		if err == nil {
			ok, err := s.load(session)
			session.IsNew = !(err == nil && ok) // not new if no error and data available
		}
	}
	return session, err
}

// Save adds a single session to the response.
func (s *RethinkStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	// Marked for deletion.
	if session.Options.MaxAge < 0 {
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
	} else {
		// Build an alphanumeric key for the redis store.
		if session.ID == "" {
			session.ID = strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)), "=")
		}
		if err := s.save(session); err != nil {
			return err
		}
		encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, s.Codecs...)
		if err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	}
	return nil
}

// MaxAge sets the maximum age for the store and the underlying cookie
// implementation. Individual sessions can be deleted by setting Options.MaxAge
// = -1 for that session.
func (s *RethinkStore) MaxAge(age int) {
	s.Options.MaxAge = age

	// Set the maxAge for each securecookie instance.
	for _, codec := range s.Codecs {
		if sc, ok := codec.(*securecookie.SecureCookie); ok {
			sc.MaxAge(age)
		}
	}
}

// save stores the session in rethink.
func (s *RethinkStore) save(session *sessions.Session) error {
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	err := enc.Encode(session.Values)
	if err != nil {
		return err
	}

	age := session.Options.MaxAge
	if age == 0 {
		age = s.DefaultMaxAge
	}
	expires := time.Now().Add(time.Duration(age) * time.Second)

	_, err = r.Table(s.Table).Get(session.ID).Replace(RethinkSession{Id: session.ID, Expires: expires, Session: buf.Bytes()}).Run(s.Rethink)
	return err
}

// load reads the session from rethink.
// returns true if there is session data in the DB.
func (s *RethinkStore) load(session *sessions.Session) (bool, error) {
	var data RethinkSession
	res, err := r.Table(s.Table).Get(session.ID).Run(s.Rethink)
	if err != nil {
		return false, err
	}
	if err := res.One(&data); err != nil {
		return false, err
	}
	dec := gob.NewDecoder(bytes.NewBuffer(data.Session))
	return true, dec.Decode(&session.Values)
}

// delete removes keys from rethink
func (s *RethinkStore) delete(session *sessions.Session) error {
	_, err := r.Table(s.Table).Get(session.ID).Delete().Run(s.Rethink)
	return err
}

// Deletes expired entries
func (s *RethinkStore) DeleteExpired() error {
	_, err := r.Table(s.Table).Between(r.MinVal, r.Now(), r.BetweenOpts{Index: "expires"}).Delete().Run(s.Rethink)
	return err
}

func (s *RethinkStore) Count() (uint, error) {
	var result interface{}
	cursor, err := r.Table(s.Table).Count().Run(s.Rethink)
	if err != nil {
		return 0, err
	}

	err = cursor.One(&result)
	if err != nil {
		return 0, err
	}

	count, ok := result.(float64)
	if !ok {
		return 0, errors.New("count isn't float64")
	}

	return uint(count), nil
}
