package cli

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
}

func newUp(t *testing.T) *upEnv {
	t.Helper()
	confirmAfter = 50 * time.Millisecond
	t.Cleanup(func() { confirmAfter = 0 })
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
	e := Env{
		Stdin: strings.NewReader(u.stdin), Stdout: &u.stdout, Stderr: &u.stderr,
		Getenv:       func(k string) string { return u.env[k] },
		ReadPassword: u.pwFn,
		Now:          func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		HTTPClient:   dialTo(u.srv.Listener.Addr().String()),
	}
	base := []string{"upload", "--phonebook", "Nextcloud", "--backup-dir", u.dir, "--timeout", "5s"}
	return Run(append(base, append(args, fixture)...), e)
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
		{"confirmation timeout", func(u *upEnv) { u.box.HoldImport = make(chan struct{}) }, []string{"--timeout", "400ms"}, "was not given in time"},
		{"count mismatch", func(u *upEnv) { u.box.ImportAppends = true }, nil, "count mismatch"},
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
			if !strings.Contains(se, "error: ") || !strings.Contains(se, tt.want) {
				t.Errorf("stderr %q lacks %q", se, tt.want)
			}
			if !strings.Contains(se, "Backup of the previous phonebook: "+b[0]) {
				t.Errorf("stderr %q lacks backup path", se)
			}
			if !u.box.WasLoggedOut() {
				t.Error("no logout")
			}
		})
	}
}

func TestUpload_ConfirmationHint(t *testing.T) {
	u := newUp(t)
	u.box.ImportDelay = 300 * time.Millisecond
	if code := u.run("--yes"); code != 0 {
		t.Fatalf("exit %d: %s", code, u.stderr.String())
	}
	if !strings.Contains(u.stderr.String(), "The FRITZ!Box asks for confirmation") {
		t.Errorf("stderr %q", u.stderr.String())
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
