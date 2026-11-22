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
	return &fritzfake.Box{User: "alice", Password: "s3cret", MultipartExportWorks: true, Phonebooks: books}
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
	for _, want := range []string{"STEP 1", "STEP 11", "Probe Contact 1", "Did the box ask for confirmation", "model=FRITZ!Box 7590 firmware=7.57", "other phonebooks before: ID 0: 1 contacts", "export of Test: 0 contacts", "after: 3 contacts in Test; 3 of 3 probe contacts present; import verdict: ambiguous (Test was empty before)", "filename=\"export_Test.xml\"", "unchanged", "after: 3 contacts in Test"} {
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
	b.ImportAppends = true // keeping the book's name, so only its contact count changes
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
	if !strings.Contains(r.trace, "body omitted") {
		t.Error("login page not replaced by a placeholder")
	}
	// phonebook.lua is read for the other book only (before and after), never for Test
	n := map[int]int{}
	for _, id := range b.PhonebookFetches() {
		n[id]++
	}
	if n[1] != 0 || n[0] != 2 {
		t.Errorf("phonebook.lua fetches = %v", n)
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
	if !strings.Contains(r.trace, "other phonebooks before: ID 0: 1 contacts") || !strings.Contains(r.trace, "non-Test phonebook contact counts unchanged") {
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
	if !strings.Contains(r.trace, "export of Test: 0 contacts") {
		t.Error("export of Test not performed")
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

func TestProbe_OmittedExportHeadersAndNames(t *testing.T) {
	pb := fritzfake.Phonebook{ID: 0, Name: "Privat Büro", XML: []byte(privatXML)}
	r := runProbe(t, box(pb, testPB), "yes\ny\ny\n")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
	for _, leak := range []string{"Privat", "Büro", "B\u00fcro", "Content-Disposition: attachment; filename=\"Privat"} {
		if strings.Contains(r.trace, leak) {
			t.Errorf("trace contains %q", leak)
		}
	}
	if !strings.Contains(r.trace, "filename=\"export_Test.xml\"") {
		t.Error("Test book export headers should be kept")
	}
}

func TestProbe_BooksNamedLikeKeywords(t *testing.T) {
	user := fritzfake.Phonebook{ID: 0, Name: "User", XML: []byte(privatXML)}
	step := fritzfake.Phonebook{ID: 2, Name: "Step", XML: []byte(privatXML)}
	r := runProbe(t, box(user, testPB, step), "yes\ny\ny\n")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.err)
	}
	if r.trace == "" || strings.Contains(r.trace, "otheruser-xyz") || strings.Contains(r.trace, "Secret Person") {
		t.Errorf("trace missing or leaking:\n%s", r.trace)
	}
}

func TestProbe_GuardReplacesNamesInKeptBodies(t *testing.T) {
	hidden := fritzfake.Phonebook{ID: 0, Name: "Hidden Book", XML: []byte(privatXML)}
	b := box(hidden, testPB)
	b.ImportAnswer = "<html><body>Imported into Hidden Book, thanks</body></html>"
	r := runProbe(t, b, "yes\ny\ny\n")
	if r.code != 0 || !strings.Contains(r.trace, "thanks") {
		t.Fatalf("exit %d, trace lacks the import answer", r.code)
	}
	if strings.Contains(r.trace, "Hidden") {
		t.Error("trace contains the other book's name")
	}
}

func TestProbe_BoxIgnoringIDNeverRecordsOtherBook(t *testing.T) {
	marker := `<?xml version="1.0"?><phonebooks><phonebook name="Other"><contact><carddav_uid>zz-leak-marker</carddav_uid><person><realName>Secret Person</realName></person></contact></phonebook></phonebooks>`
	for _, name := range []string{"Privat Büro", "Ab", "User"} {
		other := fritzfake.Phonebook{ID: 0, Name: name, XML: []byte(marker)}
		b := box(other, testPB)
		b.ServeOtherID = &other.ID // every export and phonebook.lua serves the other book
		r := runProbe(t, b, "yes\ny\ny\n")
		for _, leak := range []string{"zz-leak-marker", "Secret Person", "Privat", "Büro", "Ab.xml", "User.xml"} {
			if strings.Contains(r.trace, leak) {
				t.Errorf("%s: trace contains %q", name, leak)
			}
		}
		if !strings.Contains(r.trace, "WARNING: response of step") {
			t.Errorf("%s: no warning in the trace", name)
		}
	}
}

const twoContactsXML = `<?xml version="1.0"?><phonebooks><phonebook name="Test"><contact><person><realName>Old One</realName></person></contact><contact><person><realName>Old Two</realName></person></contact></phonebook></phonebooks>`

func TestProbe_ReplaceOrAppendVerdict(t *testing.T) {
	for appends, want := range map[bool]string{false: "import verdict: replaced", true: "import verdict: appended"} {
		b := box(privat, fritzfake.Phonebook{ID: 1, Name: "Test", XML: []byte(twoContactsXML)})
		b.ImportAppends = appends
		r := runProbe(t, b, "yes\ny\ny\n")
		if r.code != 0 || !strings.Contains(r.trace, want) {
			t.Errorf("appends=%v: want %q in trace (exit %d)", appends, want, r.code)
		}
	}
}

func TestProbe_UnverifiedWhenOtherCountsUnreadable(t *testing.T) {
	zero := 0
	b := box(privat, testPB)
	b.LuaForbiddenID = &zero
	r := runProbe(t, b, "yes\ny\ny\n")
	if !strings.Contains(r.trace, "UNVERIFIED: could not read the contact count of other phonebooks") || !strings.Contains(r.stdout, "UNVERIFIED") {
		t.Error("no UNVERIFIED notice")
	}
	if strings.Contains(r.trace, "counts unchanged") || strings.Contains(r.trace, "-1 contacts") || strings.Contains(r.trace, "map[") {
		t.Errorf("trace claims unchanged or shows -1:\n%s", r.trace)
	}
}

func TestProbe_VerdictIgnoresPhonebookLuaVirtualEntries(t *testing.T) {
	earlier := bookWith("Probe Contact 1 000000", "Probe Contact 2 000000", "Probe Contact 3 000000")
	b := box(privat, earlier)
	b.LuaVirtualEntries = map[int]int{1: 10} // phonebook.lua shows ten more entries for Test
	r := runProbe(t, b, "yes\ny\ny\n")
	if !strings.Contains(r.trace, "import verdict: replaced") || strings.Contains(r.trace, "unexpected") || strings.Contains(r.trace, "Virtual Entry") {
		t.Errorf("verdict depends on phonebook.lua:\n%s", r.trace)
	}
	for _, id := range b.PhonebookFetches() {
		if id == 1 {
			t.Error("Test was read via phonebook.lua")
		}
	}
}

func TestProbe_AbortsWithoutTR064(t *testing.T) {
	b := box(privat, testPB)
	b.NoTR064 = true
	r := runProbe(t, b, "yes\ny\ny\n")
	if r.code == 0 || len(b.Imports()) != 0 || !strings.Contains(r.err, "no write without a TR-064-confirmed name") {
		t.Errorf("exit %d, imports %v, stderr %q", r.code, b.Imports(), r.err)
	}
}

func bookWith(names ...string) fritzfake.Phonebook {
	x := `<?xml version="1.0"?><phonebooks><phonebook name="Test">`
	for _, n := range names {
		x += `<contact><person><realName>` + n + `</realName></person></contact>`
	}
	return fritzfake.Phonebook{ID: 1, Name: "Test", XML: []byte(x + `</phonebook></phonebooks>`)}
}

func TestProbe_VerdictCannotBeFooled(t *testing.T) {
	earlier := bookWith("Probe Contact 1 000000", "Probe Contact 2 000000", "Probe Contact 3 000000")
	cases := []struct {
		name  string
		book  fritzfake.Phonebook
		tweak func(*fritzfake.Box)
		want  string
	}{
		{"noop, earlier probe contacts", earlier, func(b *fritzfake.Box) { b.ImportRedirect = true }, "import not visible (0 of 3 present)"},
		{"noop, other contacts", bookWith("A One", "B Two"), func(b *fritzfake.Box) { b.ImportRedirect = true }, "import not visible (0 of 3 present)"},
		{"noop, empty", testPB, func(b *fritzfake.Box) { b.ImportRedirect = true }, "import not visible (0 of 3 present)"},
		{"empty before, real import", testPB, func(*fritzfake.Box) {}, "ambiguous (Test was empty before)"},
		{"unreadable before", earlier, func(b *fritzfake.Box) { b.ExportBroken = true }, "unknown (Test could not be read before or after)"},
		{"real replace", earlier, func(*fritzfake.Box) {}, "replaced"},
		{"real append", bookWith("A One", "B Two"), func(b *fritzfake.Box) { b.ImportAppends = true }, "appended"},
	}
	for _, c := range cases {
		b := box(privat, c.book)
		c.tweak(b)
		r := runProbe(t, b, "yes\ny\ny\n")
		if !strings.Contains(r.trace, "import verdict: "+c.want) {
			t.Errorf("%s: want verdict %q in trace", c.name, c.want)
		}
	}
	r := runProbe(t, box(privat, testPB), "yes\ny\ny\n")
	if !strings.Contains(r.trace, "Probe Contact 1 030405") {
		t.Error("probe contacts lack the run tag")
	}
}

func TestProbe_WarningAndUnverifiedTogether(t *testing.T) {
	zero := 0
	other := fritzfake.Phonebook{ID: 2, Name: "Other", XML: []byte(privatXML)}
	b := box(privat, testPB, other)
	b.ImportTarget = &zero // clobbers book 0, whose count changes; book 2 is unreadable
	b.ImportAppends = true
	two := 2
	b.LuaForbiddenID = &two
	r := runProbe(t, b, "yes\ny\ny\n")
	if !strings.Contains(r.trace, "WARNING: the contact count") || !strings.Contains(r.trace, "UNVERIFIED") {
		t.Error("want both notices")
	}
}

func TestProbe_NoOtherBooksNote(t *testing.T) {
	r := runProbe(t, box(testPB), "yes\ny\ny\n")
	if !strings.Contains(r.trace, "other phonebooks before: none") {
		t.Error("missing 'none'")
	}
}

func TestProbe_ServedNameMismatchIsUnverified(t *testing.T) {
	one := 1
	b := box(privat, testPB)
	b.ServeOtherID = &one // phonebook.lua serves Test's XML for the other book's URL
	r := runProbe(t, b, "yes\ny\ny\n")
	if !strings.Contains(r.trace, "UNVERIFIED") || strings.Contains(r.trace, "counts unchanged") {
		t.Error("a served name mismatch must make the other count unknown")
	}
}
