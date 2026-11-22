package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fritzbook/internal/fbxml"
	"fritzbook/internal/fritz/fritzfake"
)

const beforeXML = `<?xml version="1.0" encoding="utf-8"?><phonebooks><phonebook name="Nextcloud"><contact><person><realName>Old Person</realName></person></contact></phonebook></phonebooks>`

type upEnv struct {
	box    *fritzfake.Box
	srv    *httptest.Server
	dir    string
	stdout bytes.Buffer
	stderr bytes.Buffer
	stdin  string
	env    map[string]string
	pwFn   func() (string, error)
	stdinR io.Reader                            // overrides stdin when set
	files  []string                             // overrides the fixture when set
	hook   func(n int, req *http.Request) error // called per firmwarecfg POST (n counts from 1)
	fwN    int
}

type hookTransport struct {
	next http.RoundTripper
	u    *upEnv
}

func (h hookTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if h.u.hook != nil && req.Method == http.MethodPost && req.URL.Path == "/cgi-bin/firmwarecfg" {
		h.u.fwN++
		if err := h.u.hook(h.u.fwN, req); err != nil {
			return nil, err
		}
	}
	return h.next.RoundTrip(req)
}

type slowReader struct {
	d time.Duration
	s string
}

func (r *slowReader) Read(p []byte) (int, error) {
	time.Sleep(r.d)
	if r.s == "" {
		return 0, io.EOF
	}
	n := copy(p, r.s)
	r.s = r.s[n:]
	return n, nil
}

func newUp(t *testing.T) *upEnv {
	t.Helper()
	waitHintAfter = 50 * time.Millisecond
	t.Cleanup(func() { waitHintAfter = 0 })
	box := &fritzfake.Box{
		User: "alice", Password: "s3cret-pw", MultipartExportWorks: true,
		Phonebooks: []fritzfake.Phonebook{
			{ID: 0, Name: "Privat", XML: []byte(`<phonebooks><phonebook name="Privat"></phonebook></phonebooks>`)},
			{ID: 1, Name: "Nextcloud", XML: []byte(beforeXML)},
		},
	}
	u := &upEnv{box: box, dir: t.TempDir(), env: map[string]string{"FRITZBOX_PASSWORD": "s3cret-pw", "FRITZBOX_USER": "alice"}}
	u.srv = box.Start(t)
	return u
}

func dialTo(addr string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, addr)
	}}}
}

func (u *upEnv) run(args ...string) int {
	var in io.Reader = strings.NewReader(u.stdin)
	if u.stdinR != nil {
		in = u.stdinR
	}
	hc := dialTo(u.srv.Listener.Addr().String())
	hc.Transport = hookTransport{hc.Transport, u}
	e := Env{
		Stdin: in, Stdout: &u.stdout, Stderr: &u.stderr,
		Getenv:       func(k string) string { return u.env[k] },
		ReadPassword: u.pwFn,
		Now:          func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		HTTPClient:   hc,
	}
	files := []string{fixture}
	if u.files != nil {
		files = u.files
	}
	base := []string{"upload", "--phonebook", "Nextcloud", "--backup-dir", u.dir, "--timeout", "5s"}
	return Run(append(base, append(args, files...)...), e)
}

func (u *upEnv) backups(t *testing.T) []string {
	m, _ := filepath.Glob(filepath.Join(u.dir, "fritzbook-backup-*.xml"))
	return m
}

func has(reqs []string, s string) int {
	n := 0
	for _, r := range reqs {
		if r == s {
			n++
		}
	}
	return n
}

func TestUpload_Success(t *testing.T) {
	u := newUp(t)
	u.stdin = "y\n"
	if code := u.run(); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	b := u.backups(t)
	if len(b) != 1 || !strings.HasSuffix(b[0], "fritzbook-backup-Nextcloud-20260102-030405.xml") {
		t.Fatalf("backups %v", b)
	}
	data, _ := os.ReadFile(b[0])
	if string(data) != beforeXML {
		t.Errorf("backup content %q", data)
	}
	if fi, _ := os.Stat(b[0]); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if got := u.box.Imports(); len(got) != 1 || got[0] != 1 {
		t.Errorf("imports %v", got)
	}
	if n := has(u.box.Requests(), "POST /cgi-bin/firmwarecfg"); n != 3 {
		t.Errorf("firmwarecfg requests = %d, want export+import+export", n)
	}
	var fw []string
	for _, r := range u.box.Requests() {
		if r == "POST /cgi-bin/firmwarecfg" {
			fw = append(fw, r)
		}
	}
	if len(fw) != 3 {
		t.Errorf("firmwarecfg order %v", fw)
	}
	if !u.box.WasLoggedOut() {
		t.Error("no logout")
	}
	if !strings.Contains(u.stdout.String(), `Phonebook "Nextcloud" replaced: 2 contacts.`) {
		t.Errorf("stdout %q", u.stdout.String())
	}
	if !strings.Contains(u.stderr.String(), `Phonebook "Nextcloud" (ID 1, currently 1 entries) will be replaced by 2 contacts. Continue? [y/N] `) {
		t.Errorf("stderr %q", u.stderr.String())
	}
}

func TestUpload_PromptNo(t *testing.T) {
	for _, in := range []string{"n\n", "\n", "", "yes please\n"} {
		u := newUp(t)
		u.stdin = in
		if code := u.run(); code != 1 {
			t.Errorf("%q: exit %d", in, code)
		}
		if len(u.box.Imports()) != 0 || !u.box.WasLoggedOut() {
			t.Errorf("%q: import or no logout", in)
		}
	}
}

func TestUpload_PromptYesVariants(t *testing.T) {
	u := newUp(t)
	u.stdin = "YES\n"
	if code := u.run(); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
}

func TestUpload_Yes(t *testing.T) {
	u := newUp(t)
	u.stdin = "" // would abort if read
	if code := u.run("--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	if strings.Contains(u.stderr.String(), "Continue?") {
		t.Error("prompted despite --yes")
	}
}

func TestUpload_PasswordFromEnv(t *testing.T) {
	u := newUp(t)
	u.pwFn = func() (string, error) { t.Error("ReadPassword called"); return "", nil }
	if code := u.run("--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
}

func TestUpload_PasswordPrompt(t *testing.T) {
	u := newUp(t)
	delete(u.env, "FRITZBOX_PASSWORD")
	u.pwFn = func() (string, error) { return "s3cret-pw", nil }
	if code := u.run("--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	if !strings.Contains(u.stderr.String(), "Password for alice: ") {
		t.Errorf("stderr %q", u.stderr.String())
	}
}

func TestUpload_NoTerminalNoEnv(t *testing.T) {
	u := newUp(t)
	delete(u.env, "FRITZBOX_PASSWORD")
	if code := u.run("--yes"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(u.stderr.String(), "no terminal: set FRITZBOX_PASSWORD") {
		t.Errorf("stderr %q", u.stderr.String())
	}
	if len(u.box.Requests()) != 0 {
		t.Error("box contacted")
	}
}

func TestUpload_LoginErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(u *upEnv)
	}{
		{"wrong password", func(u *upEnv) { u.env["FRITZBOX_PASSWORD"] = "wrong-pw" }},
		{"blocked", func(u *upEnv) { u.box.BlockTime = 30 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUp(t)
			tt.setup(u)
			if code := u.run("--yes"); code != 1 {
				t.Fatalf("exit %d", code)
			}
			checkLoginFailure(t, u)
		})
	}
	t.Run("unreachable", func(t *testing.T) {
		u := newUp(t)
		u.srv.Close()
		if code := u.run("--yes"); code != 1 {
			t.Fatalf("exit %d", code)
		}
		checkLoginFailure(t, u)
	})
}

func checkLoginFailure(t *testing.T, u *upEnv) {
	t.Helper()
	se := strings.TrimRight(u.stderr.String(), "\n")
	se = se[strings.Index(se, "error: "):] // the conversion summary precedes it
	if strings.Contains(se, "\n") || !strings.HasPrefix(se, "error: ") {
		t.Errorf("stderr not a single error line: %q", se)
	}
	if strings.Contains(se, "s3cret-pw") || strings.Contains(se, "wrong-pw") {
		t.Errorf("password leaked: %q", se)
	}
	if len(u.backups(t)) != 0 || len(u.box.Imports()) != 0 {
		t.Error("backup or import after failed login")
	}
}

func TestUpload_FailureMentionsBackup(t *testing.T) {
	tests := []struct {
		name  string
		setup func(u *upEnv)
		args  []string
		want  string
	}{
		{"import rejected", func(u *upEnv) { u.box.ImportStatus = 500 }, nil, "rejected the import"},
		{"import timeout", func(u *upEnv) { u.box.HoldImport = make(chan struct{}) }, []string{"--timeout", "1s"}, "import timed out; the outcome is unknown"},
		{"count mismatch", func(u *upEnv) { u.box.ImportAppends = true }, nil, "count mismatch"},
		{"import ignored, same count", func(u *upEnv) {
			u.box.Phonebooks[1].XML = []byte(twoNamesXML)
			u.box.ImportIgnored = true
		}, nil, "does not match the upload"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := newUp(t)
			tt.setup(u)
			if code := u.run(append([]string{"--yes"}, tt.args...)...); code != 1 {
				t.Fatalf("exit %d: %s", code, u.stderr.String())
			}
			b := u.backups(t)
			if len(b) != 1 {
				t.Fatalf("backups %v", b)
			}
			se := u.stderr.String()
			msg := diagnosticLine(t, se, "error: ", b[0])
			checkNoDevicePrompt(t, msg)
			if !strings.Contains(msg, tt.want) {
				t.Errorf("diagnostic %q lacks %q", msg, tt.want)
			}
			if n := strings.Count(se, "Backup of the current phonebook: "+b[0]); n != 1 {
				t.Errorf("stderr %q: backup path printed %d times, want 1", se, n)
			}
			if !u.box.WasLoggedOut() {
				t.Error("no logout")
			}
		})
	}
}

const twoNamesXML = `<phonebooks><phonebook name="Nextcloud"><contact><person><realName>Old One</realName></person></contact><contact><person><realName>Old Two</realName></person></contact></phonebook></phonebooks>`

func TestUpload_IgnoredImportReportsDifferingNames(t *testing.T) {
	u := newUp(t)
	u.box.Phonebooks[1].XML = []byte(twoNamesXML)
	u.box.ImportIgnored = true
	if code := u.run("--yes"); code != 1 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	if se := u.stderr.String(); !strings.Contains(se, "the import was accepted but the phonebook does not match the upload (2 of 2 names differ)") {
		t.Errorf("stderr %q", se)
	}
	if strings.Contains(u.stdout.String(), "replaced") {
		t.Errorf("stdout %q", u.stdout.String())
	}
}

func TestUpload_DuplicateNamesMultiset(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "dup.vcf")
	vcf := "BEGIN:VCARD\nVERSION:3.0\nFN:Same Name\nTEL:+49301111\nEND:VCARD\nBEGIN:VCARD\nVERSION:3.0\nFN:Same Name\nTEL:+49302222\nEND:VCARD\n"
	if err := os.WriteFile(f, []byte(vcf), 0o600); err != nil {
		t.Fatal(err)
	}
	u := newUp(t)
	u.files = []string{f}
	if code := u.run("--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
}

func TestUpload_IgnoredNumberOnlyChange(t *testing.T) {
	u := newUp(t)
	f := filepath.Join(t.TempDir(), "number.vcf")
	if err := os.WriteFile(f, []byte("BEGIN:VCARD\nVERSION:3.0\nFN:Same Name\nTEL:123456\nEND:VCARD\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	u.files = []string{f}
	const old = `<phonebook name="Nextcloud"><contact><person><realName>Same Name</realName></person><telephony><number>654321</number></telephony></contact></phonebook>`
	u.box.Phonebooks[1].XML = []byte(old)
	u.box.ImportIgnored = true
	if code := u.run("--yes"); code != 1 || strings.Contains(u.stdout.String(), "replaced") {
		t.Fatalf("exit %d, stdout %q", code, u.stdout.String())
	}
	paths := u.backups(t)
	if len(paths) != 1 {
		t.Fatalf("backups: %v", paths)
	}
	if msg := diagnosticLine(t, u.stderr.String(), "error: ", paths[0]); !strings.Contains(msg, "number mismatch: 1") || strings.Contains(msg, "654321") {
		t.Errorf("diagnostic %q", msg)
	}
	if data, err := os.ReadFile(paths[0]); err != nil || string(data) != old {
		t.Errorf("backup = %q, err = %v", data, err)
	}
}

func TestContactDiff(t *testing.T) {
	entry := func(name string, nums ...string) fbxml.ContactIdentity {
		return fbxml.ContactIdentity{Name: name, Numbers: nums}
	}
	type entries = []fbxml.ContactIdentity
	for _, tc := range []struct {
		name      string
		want, got entries
		diff      int
	}{
		{"reordered", entries{entry("A", "1", "2"), entry("B", "3")}, entries{entry("B", "3"), entry("A", "2", "1")}, 0},
		{"exchanged", entries{entry("A", "1"), entry("B", "2")}, entries{entry("A", "2"), entry("B", "1")}, 2},
		{"duplicate names", entries{entry("A", "1"), entry("A", "2")}, entries{entry("A", "2"), entry("A", "2")}, 1},
		{"repeated number", entries{entry("A", "1", "1")}, entries{entry("A", "1")}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if d := contactDiff(tc.want, tc.got); d != tc.diff {
				t.Errorf("diff = %d, want %d", d, tc.diff)
			}
		})
	}
}

func TestUpload_BackupPathPrintedBeforePromptAndOnSuccess(t *testing.T) {
	u := newUp(t)
	u.stdin = "y\n"
	if code := u.run(); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	b := u.backups(t)[0]
	line := "Backup of the current phonebook: " + b
	se := u.stderr.String()
	i, j := strings.Index(se, line), strings.Index(se, "Continue?")
	if i < 0 || j < 0 || i > j {
		t.Errorf("backup line not before the prompt: %q", se)
	}
	if !strings.Contains(u.stdout.String(), b) {
		t.Errorf("stdout %q lacks the backup path", u.stdout.String())
	}
}

func TestWriteBackup_RemovesPartialFileOnSyncError(t *testing.T) {
	old := backupSync
	backupSync = func(*os.File) error { return errors.New("disk full") }
	t.Cleanup(func() { backupSync = old })
	dir := t.TempDir()
	if _, err := writeBackup(dir, "X", time.Now(), []byte("data")); err == nil {
		t.Fatal("want error")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*")); len(m) != 0 {
		t.Errorf("partial backup left behind: %v", m)
	}
}

func diagnosticLine(t *testing.T, stderr, prefix, backup string) string {
	t.Helper()
	// Error diagnostics collapse whitespace, including whitespace in a path.
	stderr = strings.NewReplacer(backup, "<backup>", oneLine(backup), "<backup>").Replace(stderr)
	for line := range strings.SplitSeq(stderr, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("no %q diagnostic in %q", prefix, stderr)
	return ""
}

func checkNoDevicePrompt(t *testing.T, text string) {
	t.Helper()
	for _, word := range []string{"confirmation", "code on a phone", "button", "second factor"} {
		if strings.Contains(strings.ToLower(text), word) {
			t.Errorf("unexpected %q in %q", word, text)
		}
	}
}

func TestUpload_WaitHint(t *testing.T) {
	u := newUp(t)
	u.box.ImportDelay = 300 * time.Millisecond
	if code := u.run("--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	b := u.backups(t)
	if len(b) != 1 {
		t.Fatalf("backups %v", b)
	}
	msg := diagnosticLine(t, u.stderr.String(), "Still waiting", b[0])
	checkNoDevicePrompt(t, msg)
	if !strings.Contains(msg, "Still waiting for the FRITZ!Box to answer the import request") {
		t.Errorf("wait hint %q", msg)
	}
}

func TestUpload_PhonebookNotFound(t *testing.T) {
	u := newUp(t)
	u.stdin = "y\n"
	code := Run([]string{"upload", "--phonebook", "Missing", "--backup-dir", u.dir, fixture}, Env{
		Stdin: strings.NewReader(""), Stdout: &u.stdout, Stderr: &u.stderr,
		Getenv: func(k string) string { return u.env[k] }, Now: time.Now,
		HTTPClient: dialTo(u.srv.Listener.Addr().String()),
	})
	if code != 1 || !strings.Contains(u.stderr.String(), `phonebook "Missing" not found`) {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	if len(u.backups(t)) != 0 || !u.box.WasLoggedOut() {
		t.Error("backup or no logout")
	}
}

func TestUpload_NoContacts(t *testing.T) {
	u := newUp(t)
	empty := filepath.Join(t.TempDir(), "empty.vcf")
	os.WriteFile(empty, nil, 0o600)
	code := Run([]string{"upload", "--phonebook", "Nextcloud", "--backup-dir", u.dir, empty}, Env{
		Stdin: strings.NewReader(""), Stdout: &u.stdout, Stderr: &u.stderr,
		Getenv: func(k string) string { return u.env[k] }, Now: time.Now,
		HTTPClient: dialTo(u.srv.Listener.Addr().String()),
	})
	if code != 1 || len(u.box.Requests()) != 0 {
		t.Fatalf("exit %d, requests %v, stderr %s", code, u.box.Requests(), u.stderr.String())
	}
}

func TestUpload_UsageErrors(t *testing.T) {
	u := newUp(t)
	mk := func() Env {
		return Env{Stdin: strings.NewReader(""), Stdout: &u.stdout, Stderr: &u.stderr,
			Getenv: func(k string) string { return u.env[k] }, Now: time.Now}
	}
	if code := Run([]string{"upload", fixture}, mk()); code != 2 { // no --phonebook
		t.Errorf("no phonebook: exit %d", code)
	}
	if code := Run([]string{"upload", "--phonebook", "X"}, mk()); code != 2 { // no files
		t.Errorf("no files: exit %d", code)
	}
	delete(u.env, "FRITZBOX_USER")
	if code := Run([]string{"upload", "--phonebook", "X", fixture}, mk()); code != 2 {
		t.Errorf("no user: exit %d", code)
	}
	if code := Run([]string{"upload", "--password", "x", "--phonebook", "X", fixture}, mk()); code != 2 {
		t.Errorf("--password: exit %d", code)
	}
}

func TestUpload_MissingBackupDir(t *testing.T) {
	u := newUp(t)
	u.dir = filepath.Join(u.dir, "nope")
	if code := u.run("--yes"); code != 1 || len(u.box.Requests()) != 0 {
		t.Fatalf("exit %d, requests %v", code, u.box.Requests())
	}
	if !strings.Contains(u.stderr.String(), "backup directory") {
		t.Errorf("stderr %q", u.stderr.String())
	}
}

func TestUpload_Verbose(t *testing.T) {
	u := newUp(t)
	if code := u.run("--yes", "--verbose"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	se := u.stderr.String()
	if !strings.Contains(se, "=== Step") {
		t.Fatalf("no trace: %q", se)
	}
	tr := se[strings.Index(se, "=== Step"):] // the prompt lines before it name the phonebook legitimately
	for _, leak := range []string{"s3cret-pw", "0123456789abcdef", "alice", "Nextcloud", "Old Person", "Erika", "Mustermann", "0000001"} {
		if strings.Contains(tr, leak) {
			t.Errorf("trace leaks %q", leak)
		}
	}
}

func TestUpload_VerboseOnFailure(t *testing.T) {
	u := newUp(t)
	u.env["FRITZBOX_PASSWORD"] = "wrong-pw"
	if code := u.run("--yes", "--verbose"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(u.stderr.String(), "=== Step") || strings.Contains(u.stderr.String(), "wrong-pw") {
		t.Errorf("stderr %q", u.stderr.String())
	}
}

func TestBackupName(t *testing.T) {
	if got := safeName("../a b/c:d"); strings.ContainsAny(got, `/\: `) || got == "" {
		t.Errorf("got %q", got)
	}
	if safeName("") == "" {
		t.Error("empty")
	}
}

func TestUpload_BackupExistsWhenImportArrives(t *testing.T) {
	u := newUp(t)
	checked := false
	u.hook = func(n int, _ *http.Request) error {
		if n == 2 { // the import
			b := u.backups(t)
			if len(b) != 1 {
				t.Errorf("backups at import: %v", b)
			} else if data, _ := os.ReadFile(b[0]); string(data) != beforeXML {
				t.Errorf("backup content %q", data)
			}
			checked = true
		}
		return nil
	}
	if code := u.run("--yes"); code != 0 || !checked {
		t.Fatalf("exit %d checked %v: %s", code, checked, u.stderr.String())
	}
}

func TestUpload_BackupNameCollision(t *testing.T) {
	u := newUp(t)
	for i := 0; i < 2; i++ {
		u.stdout.Reset()
		u.box.Phonebooks[1].XML = []byte(beforeXML)
		if code := u.run("--yes"); code != 0 {
			t.Fatalf("exit %d: %s", code, u.stderr.String())
		}
	}
	if b := u.backups(t); len(b) != 2 {
		t.Errorf("backups %v", b)
	}
}

func TestUpload_TimeoutsAreIndependentOfSetupAndPrompt(t *testing.T) {
	u := newUp(t)
	setupTimeout = 100 * time.Millisecond
	t.Cleanup(func() { setupTimeout = 60 * time.Second })
	u.stdinR = &slowReader{300 * time.Millisecond, "y\n"} // answered after the setup budget
	u.box.ImportDelay = 250 * time.Millisecond            // import also outlasts it
	if code := u.run("--timeout", "5s"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	if !strings.Contains(u.stderr.String(), "(waiting up to 5s)") {
		t.Errorf("hint: %q", u.stderr.String())
	}
}

func TestUpload_SkippedSummaryBeforePrompt(t *testing.T) {
	u := newUp(t)
	vcf := filepath.Join(t.TempDir(), "c.vcf")
	os.WriteFile(vcf, []byte("BEGIN:VCARD\nVERSION:3.0\nFN:Has Number\nTEL:+49 170 1234567\nEND:VCARD\nBEGIN:VCARD\nVERSION:3.0\nFN:No Number\nEND:VCARD\n"), 0o600)
	u.files = []string{vcf}
	u.stdin = "y\n"
	if code := u.run(); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	se := u.stderr.String()
	i, j, k := strings.Index(se, "Converted 1 contacts (1 numbers)."), strings.Index(se, "  - No Number:"), strings.Index(se, "Continue?")
	if i < 0 || j < i || k < j || !strings.Contains(se, "Skipped 1:") {
		t.Errorf("stderr %q", se)
	}
	if !strings.Contains(u.stdout.String(), "replaced: 1 contacts.") {
		t.Errorf("stdout %q", u.stdout.String())
	}
}

func TestUpload_UnexpectedPhonebook(t *testing.T) {
	u := newUp(t)
	other := 0
	u.box.ServeOtherID = &other
	if code := u.run("--yes"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if len(u.backups(t)) != 0 || len(u.box.Imports()) != 0 || !strings.Contains(u.stderr.String(), "unexpected phonebook") {
		t.Errorf("stderr %q", u.stderr.String())
	}
}

func TestUpload_BackupWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	u := newUp(t)
	if err := os.Chmod(u.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(u.dir, 0o700) })
	if code := u.run("--yes"); code != 1 || len(u.box.Imports()) != 0 {
		t.Fatalf("exit %d imports %v", code, u.box.Imports())
	}
	if !strings.Contains(u.stderr.String(), "write backup") {
		t.Errorf("stderr %q", u.stderr.String())
	}
}

func TestUpload_VerificationExportFails(t *testing.T) {
	u := newUp(t)
	u.hook = func(n int, _ *http.Request) error {
		if n == 3 {
			return errors.New("connection reset")
		}
		return nil
	}
	if code := u.run("--yes"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	se := u.stderr.String()
	b := u.backups(t)
	if len(u.box.Imports()) != 1 || len(b) != 1 || !strings.Contains(se, "import was accepted but could not be verified") || !strings.Contains(se, "Backup of the current phonebook: "+b[0]) {
		t.Errorf("stderr %q", se)
	}
}

func TestUpload_OldFirmware(t *testing.T) {
	u := newUp(t)
	u.box.OldFirmware = true
	if code := u.run("--yes"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	checkLoginFailure(t, u)
}

func TestUpload_VerboseMasksHostname(t *testing.T) {
	u := newUp(t)
	if code := u.run("--yes", "--verbose", "--host", "http://box-of-maria.example.test"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	se := u.stderr.String()
	tr := se[strings.Index(se, "=== Step"):]
	if strings.Contains(tr, "box-of-maria") || !strings.Contains(tr, "<HOST>") {
		t.Errorf("trace: %s", tr)
	}
}
