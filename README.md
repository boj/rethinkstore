# rethinkstore

A session store backend for [gorilla/sessions](http://www.gorillatoolkit.org/pkg/sessions) - [src](https://github.com/gorilla/sessions) using [Rethink](http://www.rethinkdb.com/).

## Requirements

Depends on the [gorethink](https://github.com/dancannon/gorethink) library.

## Installation

    go get github.com/boj/rethinkstore

## Documentation

Available on [godoc.org](http://www.godoc.org/github.com/boj/rethinkstore).

See http://www.gorillatoolkit.org/pkg/sessions for full documentation on underlying interface.

### Example

```go
// Fetch new store.
store, err := NewRethinkStore("127.0.0.1:28015",
                              "my-db",
                              "my-session-table",
                              5 /*MaxIdle*/,
                              5 /*MaxOpen*/,
                              []byte("secret-key"))
if err != nil {
    panic(err)
}
defer store.Close()

// Get a session.
session, err := store.Get(req, "session-key")
if err != nil {
    log.Error(err.Error())
}

// Add a value.
session.Values["foo"] = "bar"

// Save.
if err = sessions.Save(req, rsp); err != nil {
    t.Fatalf("Error saving session: %v", err)
}

// Delete session, but not from the store
session.Options.MaxAge = -1
if err = sessions.Save(req, rsp); err != nil {
    t.Fatalf("Error saving session: %v", err)
}

// Delete expired data. Call this function periodically
session.Options.MaxAge = -1
if err = store.DeleteExpired(); err != nil {
    t.Fatalf("Error saving session: %v", err)
}


```