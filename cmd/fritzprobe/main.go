// Command fritzprobe records how a real FRITZ!Box behaves, anonymized, for the fake box.
// A human runs it against a real box; it writes only to a phonebook named "Test".
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"fritzbook/internal/contacts"
	"fritzbook/internal/fbxml"
	"fritzbook/internal/fritz"
	"fritzbook/internal/trace"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

const testBook = "Test"

// Env carries the process environment so the probe is testable.
type Env struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	Getenv       func(string) string
	ReadPassword func() (string, error) // nil when stdin is not a terminal
	Now          func() time.Time
	HTTPClient   *http.Client // nil means default; tests inject the fake box's client
	TR064URL     string       // overrides the derived TR-064 URL (tests)
}

func main() {
	env := Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, Now: time.Now}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		env.ReadPassword = func() (string, error) {
			fmt.Fprint(os.Stderr, "FRITZ!Box password: ")
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			return string(b), err
		}
	}
	os.Exit(run(os.Args[1:], env))
}

func run(args []string, env Env) int {
	fs := flag.NewFlagSet("fritzprobe", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	host := fs.String("host", "http://fritz.box", "FRITZ!Box base URL")
	user := fs.String("user", "", "FRITZ!Box user name")
	outPath := fs.String("o", "", "output file (default fritzprobe-<YYYYMMDD-HHMMSS>.txt)")
	insecure := fs.Bool("insecure", false, "skip TLS certificate verification")
	timeout := fs.Duration("timeout", 5*time.Minute, "how long to wait for the import to finish")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *user == "" {
		fmt.Fprintln(env.Stderr, "fritzprobe: --user is required")
		return 2
	}
	password := env.Getenv("FRITZBOX_PASSWORD")
	if password == "" {
		if env.ReadPassword == nil {
			fmt.Fprintln(env.Stderr, "fritzprobe: set FRITZBOX_PASSWORD or run on a terminal")
			return 2
		}
		p, err := env.ReadPassword()
		if err != nil || p == "" {
			fmt.Fprintln(env.Stderr, "fritzprobe: no password given")
			return 2
		}
		password = p
	}

	var base http.RoundTripper
	if env.HTTPClient != nil && env.HTTPClient.Transport != nil {
		base = env.HTTPClient.Transport
	} else {
		t := http.DefaultTransport.(*http.Transport).Clone()
		if *insecure {
			t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit user opt-in
		}
		base = t
	}
	anon := trace.NewAnonymizer(testBook)
	anon.AddSecret(password, "PASSWORD")
	anon.AddSecret(*user, "USER")
	rec := trace.NewRecorder(base, anon)

	c, err := fritz.New(*host, &http.Client{Transport: rec})
	if err != nil {
		fmt.Fprintln(env.Stderr, "fritzprobe:", err)
		return 2
	}
	if env.TR064URL != "" {
		u, err := url.Parse(env.TR064URL)
		if err != nil {
			fmt.Fprintln(env.Stderr, "fritzprobe:", err)
			return 2
		}
		c.TR064URL = u
	}

	p := &probe{env: env, c: c, rec: rec, anon: anon, in: bufio.NewReader(env.Stdin), timeout: *timeout, user: *user, password: password}
	runErr := p.steps()
	if p.loggedIn {
		p.step(11, "logout")
		if err := c.Logout(context.Background()); err != nil {
			rec.Note("logout failed: %v", err)
		}
	}
	code := 0
	if runErr != nil {
		fmt.Fprintln(env.Stderr, "fritzprobe:", runErr)
		rec.Note("ABORTED: %v", runErr)
		code = 1
	}

	p.step(12, "write trace")
	path := *outPath
	if path == "" {
		path = "fritzprobe-" + env.Now().Format("20060102-150405") + ".txt"
	}
	var buf bytes.Buffer
	if _, err := rec.WriteTo(&buf); err != nil {
		fmt.Fprintln(env.Stderr, "fritzprobe:", err)
		return 1
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		fmt.Fprintln(env.Stderr, "fritzprobe:", err)
		return 1
	}
	fmt.Fprintln(env.Stdout, "Trace written to", path)
	fmt.Fprintln(env.Stdout, "Review this file before sharing it.")
	return code
}

type probe struct {
	env      Env
	c        *fritz.Client
	rec      *trace.Recorder
	anon     *trace.Anonymizer
	in       *bufio.Reader
	timeout  time.Duration
	user     string
	password string
	loggedIn bool
}

func (p *probe) step(n int, what string) { p.rec.Note("STEP %d: %s", n, what) }

func (p *probe) say(format string, args ...any) { fmt.Fprintf(p.env.Stdout, format+"\n", args...) }

// ask prints a question and returns the trimmed line typed; EOF yields "".
func (p *probe) ask(q string) string {
	fmt.Fprint(p.env.Stdout, q+" ")
	line, _ := p.in.ReadString('\n')
	return strings.TrimSpace(line)
}

// steps runs steps 1-10. The caller logs out and writes the trace.
func (p *probe) steps() error {
	ctx := context.Background()

	p.step(1, "login")
	if err := p.c.Login(ctx, p.user, p.password); err != nil {
		return err
	}
	p.loggedIn = true
	p.anon.AddSecret(p.c.SID, "SID")

	p.step(2, "device info")
	if model, fw, err := p.c.DeviceInfo(ctx); err != nil {
		p.rec.Note("DeviceInfo failed: %v", err)
	} else {
		p.rec.Note("DeviceInfo: model=%s firmware=%s", model, fw)
	}

	p.step(3, "TR-064 phonebook list")
	// names and urls come from TR-064, the authoritative source. Names of phonebooks other
	// than Test are used for matching only and never logged.
	names := map[int]string{}
	urls := map[int]string{}
	ids, err := p.c.PhonebookIDsTR064(ctx)
	if err != nil {
		p.rec.Note("TR-064 GetPhonebookList failed: %v", err)
		return errors.New("TR-064 did not list the phonebooks; no write without a TR-064-confirmed name for the Test phonebook")
	}
	p.rec.Note("TR-064 phonebook IDs: %v", ids)
	for _, id := range ids {
		name, u, err := p.c.PhonebookTR064(ctx, id)
		if err != nil {
			p.rec.Note("TR-064 GetPhonebook %d failed: %v", id, err)
			// the unknown book might be Test; do not go on to a write
			return fmt.Errorf("cannot read the name of phonebook ID %d, aborting before any write", id)
		}
		names[id], urls[id] = name, u
		p.guardName(name)
	}

	p.step(4, "pick the phonebook named 'Test'")
	var testIDs []int
	others := map[int]string{} // non-Test phonebooks: only IDs and counts are ever logged
	for id, name := range names {
		if name == testBook {
			testIDs = append(testIDs, id)
		} else {
			others[id] = name
		}
	}
	if len(testIDs) != 1 {
		return errors.New("Create exactly one phonebook named 'Test' first") //nolint:staticcheck // user-facing sentence
	}
	id := testIDs[0]
	p.rec.Note("Test phonebook has ID %d", id)

	p.step(5, "firmwarecfg multipart export experiment (Test only, read-only)")
	status, body, err := p.c.ExportPhonebookMultipartRaw(p.bodyCtx(ctx, testBook), id, testBook)
	switch {
	case err != nil:
		p.rec.Note("multipart export failed: %v", err)
	default:
		if info, ierr := fbxml.Inspect(bytes.NewReader(body)); ierr != nil {
			p.rec.Note("multipart export: status %d, not a phonebook", status)
		} else {
			p.rec.Note("multipart export: status %d, phonebook with %d contacts", status, info.Contacts)
		}
	}

	p.step(6, "read Test via the TR-064 phonebook URL (before) and count the other phonebooks")
	othersBefore := p.otherCounts(ctx, others, urls)
	p.noteOthers("before", othersBefore)
	before, ok := p.readTest(ctx, urls[id])
	if ok {
		p.rec.Note("before: %d contacts in Test", before.count)
	} else {
		p.rec.Note("before: could not read Test via the phonebook URL")
	}

	p.step(7, "confirm the write")
	p.say("Planned write: import 3 synthetic contacts (Probe Contact 1-3) into the phonebook %q (ID %d).", testBook, id)
	p.say("No other phonebook is touched.")
	if p.ask("Type 'yes' to continue:") != "yes" {
		p.rec.Note("human did not answer yes; nothing written")
		return errors.New("aborted, nothing was written")
	}

	p.step(8, "import")
	var cs []contacts.Contact
	for i := 1; i <= 3; i++ {
		cs = append(cs, contacts.Contact{
			Name:    fmt.Sprintf("Probe Contact %d", i),
			Numbers: []contacts.Number{{Value: fmt.Sprintf("+49 30 000000000%d", i), Type: "home", Prio: true}},
		})
	}
	var xmlBuf bytes.Buffer
	if err := fbxml.Write(&xmlBuf, testBook, cs); err != nil {
		return err
	}
	type result struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan result, 1)
	ictx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	start := time.Now()
	go func() {
		st, b, err := p.c.ImportPhonebookRaw(trace.ExpectPhonebook(trace.KeepBody(ictx), testBook, false), id, xmlBuf.Bytes())
		done <- result{st, b, err}
	}()
	p.say("If your phone or the FRITZ!Box asks for a confirmation now, confirm it.")
	select {
	case r := <-done:
		p.rec.Note("import finished after %s: status %d, error %v", time.Since(start).Round(time.Millisecond), r.status, r.err)
	case <-ictx.Done():
		<-done // the cancelled request returns promptly
		p.rec.Note("import did not finish within %s (cancelled)", p.timeout)
	}

	p.step(9, "human observations")
	p.rec.Note("Did the box ask for confirmation? %s", p.ask("Did the box ask for confirmation? (y/n)"))
	p.rec.Note("Did you confirm it? %s", p.ask("Did you confirm it? (y/n)"))

	p.step(10, "read Test via the TR-064 phonebook URL (after)")
	if after, ok := p.readTest(ctx, urls[id]); !ok {
		p.rec.Note("after: could not read Test via the phonebook URL")
	} else {
		present := 0
		for i := 1; i <= 3; i++ {
			if bytes.Contains(after.body, []byte(fmt.Sprintf("Probe Contact %d", i))) {
				present++
			}
		}
		prev := after.count - present
		verdict := "replaced"
		if prev > 0 {
			verdict = "appended"
		}
		p.rec.Note("after: %d contacts in Test; %d of 3 probe contacts present; %d previous contacts remain (before: %d) -> import %s the contents", after.count, present, prev, before.count, verdict)
	}
	othersAfter := p.otherCounts(ctx, others, urls)
	p.noteOthers("after", othersAfter)
	var changed []int
	unknown := false
	for id, b := range othersBefore {
		a := othersAfter[id]
		switch {
		case !b.ok || !a.ok:
			unknown = true
		case a.count != b.count:
			changed = append(changed, id)
		}
	}
	sort.Ints(changed)
	switch {
	case len(changed) > 0:
		p.rec.Note("WARNING: the contact count of non-Test phonebook IDs %v changed during the import; the box may ignore PhonebookId. Restore them from your backup.", changed)
		p.say("WARNING: other phonebooks (IDs %v) changed. Restore them from your backup.", changed)
	case unknown:
		msg := "UNVERIFIED: could not read the contact count of other phonebooks before/after the import - compare them manually against your backup"
		p.rec.Note("%s", msg)
		p.say("%s", msg)
	case len(others) == 0:
		p.rec.Note("no other phonebooks exist")
	default:
		p.rec.Note("non-Test phonebook contact counts unchanged")
	}
	return nil
}

type count struct {
	count int
	ok    bool
	name  string
	body  []byte
}

// otherCounts reads each non-Test phonebook through its TR-064 URL with the body omitted
// and returns the contact counts. Names and contact data are never recorded.
func (p *probe) otherCounts(ctx context.Context, names map[int]string, urls map[int]string) map[int]count {
	counts := map[int]count{}
	for id := range names {
		c, _ := p.fetchBook(trace.OmitBody(ctx), urls[id])
		c.body = nil
		counts[id] = c
	}
	return counts
}

// noteOthers records the known counts (never names); unknown ones are said to be unknown.
func (p *probe) noteOthers(when string, m map[int]count) {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var parts []string
	for _, id := range ids {
		if m[id].ok {
			parts = append(parts, fmt.Sprintf("ID %d: %d contacts", id, m[id].count))
		} else {
			parts = append(parts, fmt.Sprintf("ID %d: unknown", id))
		}
	}
	p.rec.Note("other phonebooks %s: %s", when, strings.Join(parts, "; "))
}

// readTest reads the Test phonebook via its URL, keeping the body in the trace unless the
// box answers with another phonebook (trace.ExpectPhonebook).
func (p *probe) readTest(ctx context.Context, u string) (count, bool) {
	c, status := p.fetchBook(trace.ExpectPhonebook(trace.KeepBody(ctx), testBook, true), u)
	if c.ok && c.name != testBook {
		p.rec.Note("WARNING: the phonebook URL of Test served another phonebook")
		c = count{}
	}
	p.rec.Note("phonebook URL fetch of Test: status %d, parsed=%v, contacts=%d", status, c.ok, c.count)
	return c, c.ok
}

// fetchBook does a read-only GET of a NewPhonebookURL and parses the contact count.
func (p *probe) fetchBook(ctx context.Context, u string) (count, int) {
	if u == "" {
		return count{}, 0
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return count{}, 0
	}
	// the box chooses this host; keep a host name out of the request line and Referer headers
	if h := req.URL.Hostname(); net.ParseIP(h) == nil {
		p.anon.AddSecret(h, "HOST")
		p.anon.AddSecret(req.URL.Host, "HOST")
	}
	res, err := p.c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL carries the SID
		}
		p.rec.Note("phonebook URL fetch failed: %v", err)
		return count{}, 0
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if res.StatusCode != http.StatusOK {
		return count{}, res.StatusCode
	}
	info, err := fbxml.Inspect(bytes.NewReader(body))
	if err != nil {
		return count{}, res.StatusCode
	}
	return count{count: info.Contacts, ok: true, name: info.Name, body: body}, res.StatusCode
}

// Error text in notes (p.rec.Note("... failed: %v", err)) cannot carry box data: fritz.Client.do
// reduces transport errors to their cause (no URL), the raw calls add only status lines, and
// TR-064 faults are the box's fixed UPnP error strings. Notes also go through Apply, so
// guarded names are replaced there as well.

// guardName registers the name of a phonebook other than Test as a secret, a backstop on top
// of the body omission. Names shorter than 3 characters would damage ordinary text.
func (p *probe) guardName(name string) {
	if name != testBook {
		p.anon.AddGuard(name)
	}
}

// bodyCtx keeps response bodies only for the Test phonebook.
func (p *probe) bodyCtx(ctx context.Context, name string) context.Context {
	if name == testBook {
		return trace.ExpectPhonebook(ctx, testBook, true)
	}
	return trace.OmitBody(ctx)
}
