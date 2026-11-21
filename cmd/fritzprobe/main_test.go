package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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
	return runProbeWith(t, b, stdin, nil)
}

func runProbeWith(t *testing.T, b *fritzfake.Box, stdin string, tweak func(*Env, *httptest.Server)) result {
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
	if tweak != nil {
		tweak(&env, srv)
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
	for _, leak := range []string{"Secret Person", "Privat", "5551234", "s3cret", "0123456789abcdef", "Johns-iPhone", "jdoe", "5559999"} {
		if strings.Contains(r.trace, leak) {
			t.Errorf("trace contains %q", leak)
		}
	}
	for _, want := range []string{"STEP 1", "STEP 12", "Probe Contact 1", "Did the box ask for confirmation", "model=FRITZ!Box 7590 firmware=7.57", "ID 0: export ok, 1 contacts", "phonebook URL fetch: status 200", "unchanged", "after: 3 contacts"} {
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

func TestProbe_WarnsWhenOtherPhonebookChanges(t *testing.T) {
	zero := 0
	b := box(privat, testPB)
	b.ImportTarget = &zero // the fake box ignores PhonebookId and writes to Privat
	r := runProbe(t, b, "yes\ny\ny\n")
	if !strings.Contains(r.trace, "WARNING: the contact count of non-Test phonebook IDs [0] changed") {
		t.Errorf("trace lacks the warning:\n%s", r.trace)
	}
	if !strings.Contains(r.stdout, "WARNING") {
		t.Errorf("stdout lacks the warning: %q", r.stdout)
	}
	if strings.Contains(r.trace, "Secret Person") {
		t.Error("trace leaks private contact")
	}
}

func TestProbe_FindsTestViaTR064WhenExportListingFails(t *testing.T) {
	b := box(privat, testPB)
	b.ExportBroken = true // every export is bounced to the login page
	r := runProbe(t, b, "yes\ny\ny\n")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
	if got := b.Imports(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("imports = %v, want [1]", got)
	}
	for _, leak := range []string{"Maria Example", "Max Muster", "boxuser", "Secret Person", "Privat", "AABBCC"} {
		if strings.Contains(r.trace, leak) {
			t.Errorf("trace contains %q", leak)
		}
	}
	if !strings.Contains(r.trace, "HTML body omitted") {
		t.Error("login page not replaced by a placeholder")
	}
	// the TR-064 URL is fetched for Test only
	n := 0
	for _, q := range b.Requests() {
		if q == "GET /phonebook.lua" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("phonebook.lua requests = %d, want 1", n)
	}
}

func TestProbe_KeepsImportAnswerButRedactsIt(t *testing.T) {
	b := box(privat, testPB)
	b.ImportAnswer = `<html><head><title>Confirm</title></head><body>Please confirm on your phone.<script>var d={"activeUsers":[{"value":"Maria Example","text":"Maria Example","UID":"boxuser1"}],"challenge":"2$10000$AABBCC$2000$DDEEFF"}; var u="/x?sid=fedcba9876543210";</script></body></html>`
	r := runProbe(t, b, "yes\ny\ny\n")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
	if !strings.Contains(r.trace, "Please confirm on your phone.") {
		t.Error("import answer not recorded")
	}
	for _, leak := range []string{"Maria Example", "boxuser", "AABBCC", "fedcba9876543210"} {
		if strings.Contains(r.trace, leak) {
			t.Errorf("trace contains %q", leak)
		}
	}
}

func TestProbe_NonTestBooksNeverRecorded(t *testing.T) {
	hidden := fritzfake.Phonebook{ID: 0, Name: "Hidden Book", XML: []byte(`<?xml version="1.0"?><phonebooks><phonebook name='Hidden Book'><contact><carddav_uid>uid-secret-123</carddav_uid><person><realName><![CDATA[Cdata Person]]></realName><vanity>vanity-xyz</vanity></person></contact></phonebook></phonebooks>`)}
	r := runProbe(t, box(hidden, testPB), "yes\ny\ny\n")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
	for _, leak := range []string{"Cdata Person", "uid-secret-123", "vanity-xyz", "Hidden", "carddav"} {
		if strings.Contains(r.trace, leak) {
			t.Errorf("trace contains %q", leak)
		}
	}
	if !strings.Contains(r.trace, "[response body omitted:") {
		t.Error("non-Test export bodies should be omitted")
	}
	if !strings.Contains(r.trace, "ID 0: export ok, 1 contacts") || !strings.Contains(r.trace, "non-Test phonebook contact counts unchanged") {
		t.Error("counts missing")
	}
}

func TestProbe_RedirectedImportAnswerSummarized(t *testing.T) {
	b := box(privat, testPB)
	b.ImportRedirect = true
	r := runProbe(t, b, "yes\ny\ny\n")
	for _, leak := range []string{"Maria Example", "Max Muster", "boxuser", "AABBCC"} {
		if strings.Contains(r.trace, leak) {
			t.Errorf("trace contains %q", leak)
		}
	}
	if !strings.Contains(r.trace, "[HTML body omitted:") {
		t.Error("redirected login page should be summarized")
	}
}

func TestProbe_PhonebookURLHostnameRedacted(t *testing.T) {
	b := box(privat, testPB)
	b.PhonebookHost = "box-of-maria.example.test"
	r := runProbeWith(t, b, "yes\ny\ny\n", func(env *Env, srv *httptest.Server) {
		addr := srv.Listener.Addr().String()
		env.HTTPClient = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}}}
	})
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
	if strings.Contains(r.trace, "maria") {
		t.Errorf("trace contains the host name")
	}
	if !strings.Contains(r.trace, "phonebook URL fetch: status 200") {
		t.Error("URL fetch not performed")
	}
}

func TestProbe_AbortsWhenGetPhonebookFails(t *testing.T) {
	for _, bad := range []int{0, 1} {
		b := box(privat, testPB)
		b.BrokenBookID = &bad
		r := runProbe(t, b, "yes\ny\ny\n")
		if r.code == 0 {
			t.Errorf("bad=%d: exit 0, want failure", bad)
		}
		if got := b.Imports(); len(got) != 0 {
			t.Errorf("bad=%d: imports = %v", bad, got)
		}
	}
}
