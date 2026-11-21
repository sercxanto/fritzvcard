// Package fritzfake is a fake FRITZ!Box for tests. One httptest server serves the web
// interface and the TR-064 paths; every request is logged as "METHOD path".
package fritzfake

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"fritzbook/internal/fritz"
)

const (
	emptySID     = "0000000000000000"
	validSID     = "0123456789abcdef"
	challenge    = "2$10000$5A1711$2000$5A1722"
	oldChallenge = "1234567890abcdef1234567890abcdef" // pre-7.24 style hex challenge
)

// Phonebook is a phonebook held by the fake box.
type Phonebook struct {
	ID   int
	Name string
	XML  []byte
}

// Box is the fake. Configure the exported fields before Start; read Requests and
// LoggedOut after the client is done.
type Box struct {
	User, Password string
	Phonebooks     []Phonebook
	BlockTime      int  // seconds; >0 makes the login endpoint report a block
	OldFirmware    bool // serve a pre-7.24 challenge
	LoggedOut      bool // set when a logout request with the valid SID arrived

	// Requests lists "METHOD path" in arrival order, for order assertions.
	Requests []string

	mu sync.Mutex
}

// Start serves the box until the test ends. Point Client.TR064URL at the returned URL too.
func (b *Box) Start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login_sid.lua", b.handleLogin)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.Requests = append(b.Requests, r.Method+" "+r.URL.Path)
		b.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (b *Box) handleLogin(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("logout") == "1" {
		b.mu.Lock()
		b.LoggedOut = q.Get("sid") == validSID
		b.mu.Unlock()
		writeSession(w, emptySID, "", 0)
		return
	}
	ch := challenge
	if b.OldFirmware {
		ch = oldChallenge
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		want, err := fritz.SolveChallenge(challenge, b.Password)
		if b.BlockTime == 0 && err == nil && r.PostForm.Get("username") == b.User && r.PostForm.Get("response") == want {
			writeSession(w, validSID, "", 0)
			return
		}
		writeSession(w, emptySID, ch, b.BlockTime)
		return
	}
	writeSession(w, emptySID, ch, b.BlockTime)
}

func writeSession(w http.ResponseWriter, sid, challenge string, block int) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><SessionInfo><SID>%s</SID><Challenge>%s</Challenge><BlockTime>%s</BlockTime></SessionInfo>`,
		sid, challenge, strconv.Itoa(block))
}
