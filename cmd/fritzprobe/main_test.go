package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fritzbook/internal/fritz/fritzfake"
)

const privatXML = `<?xml version="1.0"?><phonebooks><phonebook name="Privat"><contact><person><realName>Secret Person</realName></person><telephony><number type="home">+49 30 5551234</number></telephony></contact></phonebook></phonebooks>`

func box(books ...fritzfake.Phonebook) *fritzfake.Box {
	return &fritzfake.Box{User: "alice", Password: "s3cret", Phonebooks: books}
}

var (
	privat = fritzfake.Phonebook{ID: 0, Name: "Privat", XML: []byte(privatXML)}
	testPB = fritzfake.Phonebook{ID: 1, Name: "Test", XML: []byte(`<?xml version="1.0"?><phonebooks><phonebook name="Test"></phonebook></phonebooks>`)}
)

type result struct {
	code        int
	stdout, err string
	trace       string
}

func runProbe(t *testing.T, b *fritzfake.Box, stdin string) result {
	t.Helper()
	srv := b.Start(t)
	out := filepath.Join(t.TempDir(), "trace.txt")
	var so, se bytes.Buffer
	env := Env{
		Stdin:      strings.NewReader(stdin),
		Stdout:     &so,
		Stderr:     &se,
		Getenv:     func(k string) string { return map[string]string{"FRITZBOX_PASSWORD": "s3cret"}[k] },
		Now:        func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		HTTPClient: srv.Client(),
		TR064URL:   srv.URL,
	}
	code := run([]string{"--host", srv.URL, "--user", "alice", "-o", out, "--timeout", "10s"}, env)
	tr, _ := os.ReadFile(out)
	return result{code, so.String(), se.String(), string(tr)}
}

func TestProbe_RefusesWithoutTestPhonebook(t *testing.T) {
	for name, b := range map[string]*fritzfake.Box{
		"none": box(privat),
		"two":  box(testPB, fritzfake.Phonebook{ID: 2, Name: "Test", XML: testPB.XML}),
	} {
		b := b
		t.Run(name, func(t *testing.T) {
			r := runProbe(t, b, "yes\ny\ny\n")
			if r.code == 0 {
				t.Error("exit code 0, want failure")
			}
			if !strings.Contains(r.err, "Create exactly one phonebook named 'Test' first") {
				t.Errorf("stderr = %q", r.err)
			}
			if got := b.Imports(); len(got) != 0 {
				t.Errorf("imports = %v, want none", got)
			}
			if !b.WasLoggedOut() {
				t.Error("session not closed")
			}
		})
	}
}

func TestProbe_WritesOnlyToTest(t *testing.T) {
	b := box(privat, testPB)
	r := runProbe(t, b, "yes\ny\ny\n")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
	if got := b.Imports(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("imports = %v, want [1]", got)
	}
	for _, leak := range []string{"Secret Person", "5551234", "s3cret", "0123456789abcdef"} {
		if strings.Contains(r.trace, leak) {
			t.Errorf("trace contains %q", leak)
		}
	}
	for _, want := range []string{"STEP 1", "STEP 12", "Probe Contact 1", "Did the box ask for confirmation", "model=", "after: 3 contacts"} {
		if !strings.Contains(r.trace, want) {
			t.Errorf("trace lacks %q", want)
		}
	}
	for _, want := range []string{"If your phone or the FRITZ!Box asks for a confirmation now, confirm it.", "Review this file before sharing it."} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %q", want)
		}
	}
	if !b.WasLoggedOut() {
		t.Error("session not closed")
	}
}

func TestProbe_AbortsWithoutYes(t *testing.T) {
	for _, in := range []string{"no\n", "", "y\n"} {
		b := box(privat, testPB)
		r := runProbe(t, b, in)
		if r.code == 0 {
			t.Errorf("stdin %q: exit 0, want failure", in)
		}
		if got := b.Imports(); len(got) != 0 {
			t.Errorf("stdin %q: imports = %v", in, got)
		}
		if !b.WasLoggedOut() {
			t.Errorf("stdin %q: session not closed", in)
		}
	}
}
