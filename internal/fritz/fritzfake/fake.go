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

// Box is the fake. Configure the exported fields before Start; read the recorded state
// through the locked accessors Requests, WasLoggedOut, Imports and PhonebookXML.
type Box struct {
	User, Password string
	Phonebooks     []Phonebook
	BlockTime      int    // seconds; >0 makes the login endpoint report a block
	OldFirmware    bool   // serve a pre-7.24 challenge
	Model          string // DeviceInfo model; default "FRITZ!Box 7590"
	Firmware       string // DeviceInfo FRITZ!OS version; default "7.57"
	ExportBroken   bool   // every firmwarecfg export is answered with the login redirect
	ImportTarget   *int   // if set, imports land in this phonebook ID instead (simulates a box ignoring PhonebookId)

	mu        sync.Mutex
	requests  []string
	loggedOut bool
	imports   []int
}

// Requests returns a copy of the "METHOD path" log in arrival order.
func (b *Box) Requests() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.requests...)
}

// WasLoggedOut reports whether a logout request with the valid SID arrived.
func (b *Box) WasLoggedOut() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loggedOut
}

// Imports returns the phonebook IDs that received a firmwarecfg import, in order.
func (b *Box) Imports() []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]int(nil), b.imports...)
}

// PhonebookXML returns a copy of the XML currently held for phonebook id, or nil.
func (b *Box) PhonebookXML(id int) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, p := range b.Phonebooks {
		if p.ID == id {
			return append([]byte(nil), p.XML...)
		}
	}
	return nil
}

// Start serves the box until the test ends. Point Client.TR064URL at the returned URL too.
func (b *Box) Start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login_sid.lua", b.handleLogin)
	mux.HandleFunc("/upnp/control/x_contact", b.handleTR064)
	mux.HandleFunc("/upnp/control/deviceinfo", b.handleTR064)
	mux.HandleFunc("/cgi-bin/firmwarecfg", b.handleFirmwareCfg)
	mux.HandleFunc("/index.lua", b.handleIndex)
	mux.HandleFunc("/phonebook.lua", b.handlePhonebookURL)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.requests = append(b.requests, r.Method+" "+r.URL.Path)
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
		b.loggedOut = q.Get("sid") == validSID
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
