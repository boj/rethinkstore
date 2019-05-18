package rethinkstore

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"testing"
	"time"

	r "github.com/dancannon/gorethink"
	"github.com/gorilla/sessions"
)

var (
	TestDatabase string = "session_test_db"
	TestTable    string = "session_test_table"
)

// ----------------------------------------------------------------------------
// ResponseRecorder
// ----------------------------------------------------------------------------
// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// ResponseRecorder is an implementation of http.ResponseWriter that
// records its mutations for later inspection in tests.
type ResponseRecorder struct {
	Code      int           // the HTTP response code from WriteHeader
	HeaderMap http.Header   // the HTTP response headers
	Body      *bytes.Buffer // if non-nil, the bytes.Buffer to append written data to
	Flushed   bool
}

// NewRecorder returns an initialized ResponseRecorder.
func NewRecorder() *ResponseRecorder {
	return &ResponseRecorder{
		HeaderMap: make(http.Header),
		Body:      new(bytes.Buffer),
	}
}

// DefaultRemoteAddr is the default remote address to return in RemoteAddr if
// an explicit DefaultRemoteAddr isn't set on ResponseRecorder.
const DefaultRemoteAddr = "1.2.3.4"

// Header returns the response headers.
func (rw *ResponseRecorder) Header() http.Header {
	return rw.HeaderMap
}

// Write always succeeds and writes to rw.Body, if not nil.
func (rw *ResponseRecorder) Write(buf []byte) (int, error) {
	if rw.Body != nil {
		rw.Body.Write(buf)
	}
	if rw.Code == 0 {
		rw.Code = http.StatusOK
	}
	return len(buf), nil
}

// WriteHeader sets rw.Code.
func (rw *ResponseRecorder) WriteHeader(code int) {
	rw.Code = code
}

// Flush sets rw.Flushed to true.
func (rw *ResponseRecorder) Flush() {
	rw.Flushed = true
}

// ----------------------------------------------------------------------------

func rethinkSession() *r.Session {
	session, err := r.Connect(r.ConnectOpts{
		Address:  "127.0.0.1:28015",
		Database: TestDatabase,
		MaxIdle:  5,
		MaxOpen:  5,
	})
	if err != nil {
		panic(err)
	}
	return session
}

func Setup() error {
	if err := r.DBCreate(TestDatabase).Exec(rethinkSession()); err != nil {
		return err
	}
	return r.DB(TestDatabase).TableCreate(TestTable).Exec(rethinkSession())
}

func Teardown() error {
	if err := r.DB(TestDatabase).TableDrop(TestTable).Exec(rethinkSession()); err != nil {
		return err
	}
	return r.DBDrop(TestDatabase).Exec(rethinkSession())
}

type FlashMessage struct {
	Type    int
	Message string
}

func TestRethinkStore(t *testing.T) {
	var req *http.Request
	var rsp *ResponseRecorder
	var hdr http.Header
	var err error
	var ok bool
	var cookies []string
	var session *sessions.Session
	var flashes []interface{}

	// Copyright 2012 The Gorilla Authors. All rights reserved.
	// Use of this source code is governed by a BSD-style
	// license that can be found in the LICENSE file.

	// Teardown just in case
	Teardown()
	if err := Setup(); err != nil {
		panic(err)
	}

	// Round 1 ----------------------------------------------------------------

	// RethinkStore
	store, err := NewRethinkStore("127.0.0.1:28015", TestDatabase, TestTable, 5, 5, []byte("secret-key"))
	if err != nil {
		t.Fatal(err.Error())
	}
	defer store.Close()

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	rsp = NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Get a flash.
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("Expected empty flashes; Got %v", flashes)
	}
	// Add some flashes.
	session.AddFlash("foo")
	session.AddFlash("bar")
	// Custom key.
	session.AddFlash("baz", "custom_key")
	// Save.
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}
	hdr = rsp.Header()
	cookies, ok = hdr["Set-Cookie"]
	if !ok || len(cookies) != 1 {
		t.Fatalf("No cookies. Header: %s", hdr)
	}

	// Round 2 ----------------------------------------------------------------

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	req.Header.Add("Cookie", cookies[0])
	rsp = NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Check all saved values.
	flashes = session.Flashes()
	if len(flashes) != 2 {
		t.Fatalf("Expected flashes; Got %v", flashes)
	}
	if flashes[0] != "foo" || flashes[1] != "bar" {
		t.Errorf("Expected foo,bar; Got %v", flashes)
	}
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("Expected dumped flashes; Got %v", flashes)
	}
	// Custom key.
	flashes = session.Flashes("custom_key")
	if len(flashes) != 1 {
		t.Errorf("Expected flashes; Got %v", flashes)
	} else if flashes[0] != "baz" {
		t.Errorf("Expected baz; Got %v", flashes)
	}
	flashes = session.Flashes("custom_key")
	if len(flashes) != 0 {
		t.Errorf("Expected dumped flashes; Got %v", flashes)
	}

	// RethinkStore specific
	// Set MaxAge to -1 to mark for deletion.
	session.Options.MaxAge = -1
	// Save.
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}

	// Round 3 ----------------------------------------------------------------
	// Custom type

	// RethinkStore
	store, err = NewRethinkStore("127.0.0.1:28015", TestDatabase, TestTable, 5, 5, []byte("secret-key"))
	if err != nil {
		panic(err)
	}
	defer store.Close()

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	rsp = NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Get a flash.
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("Expected empty flashes; Got %v", flashes)
	}
	// Add some flashes.
	session.AddFlash(&FlashMessage{42, "foo"})
	// Save.
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}
	hdr = rsp.Header()
	cookies, ok = hdr["Set-Cookie"]
	if !ok || len(cookies) != 1 {
		t.Fatalf("No cookies. Header: %s", hdr)
	}

	// Round 4 ----------------------------------------------------------------
	// Custom type

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	req.Header.Add("Cookie", cookies[0])
	rsp = NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Check all saved values.
	flashes = session.Flashes()
	if len(flashes) != 1 {
		t.Fatalf("Expected flashes; Got %v", flashes)
	}
	custom := flashes[0].(FlashMessage)
	if custom.Type != 42 || custom.Message != "foo" {
		t.Errorf("Expected %#v, got %#v", FlashMessage{42, "foo"}, custom)
	}

	// RethinkStore specific
	// Set MaxAge to -1 to mark for deletion.
	session.Options.MaxAge = -1
	// Save.
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}

	if err := Teardown(); err != nil {
		panic(err)
	}
}

func ExampleRethinkStore() {
	// RethinkStore
	store, err := NewRethinkStore("127.0.0.1:28015", TestDatabase, TestTable, 5, 5, []byte("secret-key"))
	if err != nil {
		panic(err)
	}
	defer store.Close()
}

func init() {
	gob.Register(FlashMessage{})
}

func TestDeleteExpiredFromEmpty(t *testing.T) {
	store, err := NewRethinkStore("127.0.0.1:28015", TestDatabase, TestTable, 5, 5, []byte("secret-key"))
	if err != nil {
		t.Fatalf(err.Error())
	}
	defer store.Close()

	// Delete from empty
	store.DeleteExpired()

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Error in count, %s", err.Error())
	}

	if count != 0 {
		t.Fatalf("Non zero count")
	}
}

func TestDeleteExpired(t *testing.T) {
	var req *http.Request
	var rsp *ResponseRecorder
	var err error
	var session *sessions.Session
	var flashes []interface{}

	store, err := NewRethinkStore("127.0.0.1:28015", TestDatabase, TestTable, 5, 5, []byte("secret-key"))
	if err != nil {
		t.Fatalf(err.Error())
	}
	defer store.Close()

	req, _ = http.NewRequest("GET", "http://localhost:8080/", nil)
	rsp = NewRecorder()
	// Get a session.
	if session, err = store.Get(req, "session-key"); err != nil {
		t.Fatalf("Error getting session: %v", err)
	}
	// Get a flash.
	flashes = session.Flashes()
	if len(flashes) != 0 {
		t.Errorf("Expected empty flashes; Got %v", flashes)
	}
	// Add some flashes.
	session.AddFlash("foo")

	// Save with the smallest possible MaxAge
	session.Options.MaxAge = 0
	if err = sessions.Save(req, rsp); err != nil {
		t.Fatalf("Error saving session: %v", err)
	}

	count, err := store.Count()
	if err != nil {
		t.Fatalf("Error in count, %s", err.Error())
	}

	if count != 1 {
		t.Fatalf("Bad count")
	}

	time.Sleep(time.Second)

	// Delete from empty
	store.DeleteExpired()

	count, err = store.Count()
	if err != nil {
		t.Fatalf("Error in count, %s", err.Error())
	}

	if count != 0 {
		t.Fatalf("Bad count: %d", count)
	}

	Teardown()
}
