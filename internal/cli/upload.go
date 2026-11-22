package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fritzbook/internal/contacts"
	"fritzbook/internal/fbxml"
	"fritzbook/internal/fritz"
	"fritzbook/internal/trace"
)

// setupTimeout bounds login, phonebook lookup and the backup export; verifyTimeout the
// verification export after an accepted import. Variables so tests can shrink them.
var (
	setupTimeout  = 60 * time.Second
	verifyTimeout = 60 * time.Second
)

// confirmAfter overrides Client.ConfirmAfter when non-zero; tests shorten it.
var confirmAfter time.Duration

func runUpload(args []string, env Env) int {
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	pbName := fs.String("phonebook", "", "name of the phonebook to replace (required)")
	host := fs.String("host", "http://fritz.box", "FRITZ!Box address")
	user := fs.String("user", "", "FRITZ!Box user (default $FRITZBOX_USER)")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	backupDir := fs.String("backup-dir", ".", "directory for the backup of the previous phonebook")
	timeout := fs.Duration("timeout", 5*time.Minute, "overall timeout, also the wait for the box's confirmation")
	insecure := fs.Bool("insecure", false, "skip TLS certificate verification")
	verbose := fs.Bool("verbose", false, "print an anonymized request trace to stderr at exit")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: fritzbook upload --phonebook <name> [flags] <file.vcf>...\n(password: $FRITZBOX_PASSWORD or prompt)")
		fs.PrintDefaults()
	}
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	usageErr := func(msg string) int {
		fmt.Fprintf(env.Stderr, "fritzbook upload: %s\n", msg)
		fs.Usage()
		return 2
	}
	if *pbName == "" {
		return usageErr("--phonebook is required")
	}
	if len(files) == 0 {
		return usageErr("at least one .vcf file is required")
	}
	if *user == "" {
		*user = env.Getenv("FRITZBOX_USER")
	}
	if *user == "" {
		return usageErr("--user or FRITZBOX_USER is required")
	}

	u := &uploader{env: env, phonebook: *pbName, user: *user, yes: *yes, backupDir: *backupDir, timeout: *timeout}
	var rec *trace.Recorder
	if *verbose {
		u.anon = trace.NewAnonymizer("")
		u.anon.AddSecret(*user, "USER")
		u.anon.AddGuard(*pbName)
		if pu, err := url.Parse(*host); err == nil && pu.Hostname() != "" && net.ParseIP(pu.Hostname()) == nil {
			u.anon.AddSecret(pu.Hostname(), "HOST") // the anonymizer only masks IP addresses
			u.anon.AddSecret(pu.Host, "HOST")
		}
		defer func() {
			fmt.Fprintln(env.Stderr, "--- trace (anonymized) ---")
			if _, err := rec.WriteTo(env.Stderr); err != nil {
				fmt.Fprintf(env.Stderr, "trace not written: %v\n", err)
			}
		}()
	}
	u.httpClient, rec = buildHTTPClient(env.HTTPClient, *insecure, u.anon)
	u.host = *host

	err = u.run(files)
	if err != nil {
		fmt.Fprintf(env.Stderr, "error: %s\n", oneLine(err.Error()))
		if u.backupPath != "" {
			fmt.Fprintf(env.Stderr, "Backup of the previous phonebook: %s\n", u.backupPath)
		}
		return 1
	}
	return 0
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// buildHTTPClient returns the client to use; with a recorder its transport is wrapped.
func buildHTTPClient(injected *http.Client, insecure bool, anon *trace.Anonymizer) (*http.Client, *trace.Recorder) {
	var hc http.Client
	if injected != nil {
		hc = *injected
	} else {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if insecure {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in via --insecure
		}
		hc = http.Client{Transport: tr} // deadlines come from the contexts
	}
	var rec *trace.Recorder
	if anon != nil {
		rec = trace.NewRecorder(hc.Transport, anon) // nil transport means the default
		hc.Transport = rec
	}
	return &hc, rec
}

type uploader struct {
	env        Env
	phonebook  string
	user       string
	yes        bool
	backupDir  string
	timeout    time.Duration
	host       string
	httpClient *http.Client
	anon       *trace.Anonymizer
	backupPath string
}

func (u *uploader) run(files []string) (err error) {
	cs, skipped, err := contacts.ParseFiles(files)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return errors.New("no contacts found in the given files; nothing to upload")
	}
	printSummary(u.env.Stderr, cs, skipped)
	var xmlBuf bytes.Buffer
	if err := fbxml.Write(&xmlBuf, u.phonebook, cs); err != nil {
		return err
	}
	newXML := xmlBuf.Bytes()
	info, err := fbxml.Inspect(bytes.NewReader(newXML))
	if err != nil {
		return err
	}
	m := info.Contacts

	if fi, err := os.Stat(u.backupDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("backup directory %q does not exist or is not a directory", u.backupDir)
	}
	pw, err := password(u.env, u.user)
	if err != nil {
		return err
	}
	if u.anon != nil {
		u.anon.AddSecret(pw, "PASSWORD")
	}

	// Login, lookup and the export run under a fixed, generous budget; --timeout only bounds
	// the import (including the wait for the box's confirmation) and starts after the prompt,
	// so the time spent at the y/N prompt cannot eat it.
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	c, err := fritz.New(u.host, u.httpClient)
	if err != nil {
		return err
	}
	c.ConfirmAfter = confirmAfter
	if err := c.Login(ctx, u.user, pw); err != nil {
		return err
	}
	if u.anon != nil {
		u.anon.AddSecret(c.SID, "SID")
	}
	defer func() { // must not hide the main error
		lctx, lcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer lcancel()
		if lerr := c.Logout(lctx); lerr != nil {
			fmt.Fprintf(u.env.Stderr, "warning: %s\n", oneLine(lerr.Error()))
		}
	}()

	pb, err := c.FindPhonebook(ctx, u.phonebook)
	if err != nil {
		return err
	}
	// Export and import bodies hold the user's real contacts: never record them.
	before, err := c.ExportPhonebook(trace.OmitBody(ctx), pb.ID, pb.Name)
	if err != nil {
		return fmt.Errorf("export of the current phonebook: %w", err)
	}
	cur, err := fbxml.Inspect(bytes.NewReader(before))
	if err != nil {
		return err
	}
	path, err := writeBackup(u.backupDir, pb.Name, u.env.Now(), before)
	if err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	u.backupPath = path

	if !u.yes {
		fmt.Fprintf(u.env.Stderr, "Phonebook %q (ID %d, currently %d entries) will be replaced by %d contacts. Continue? [y/N] ", pb.Name, pb.ID, cur.Contacts, m)
		line, _ := bufio.NewReader(u.env.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
			return errors.New("aborted, the phonebook was not changed")
		}
	}

	onConfirm := func(string) {
		fmt.Fprintf(u.env.Stderr, "The FRITZ!Box asks for confirmation: enter the code on a phone or press a button on the box (waiting up to %s)…\n", u.timeout)
	}
	// Fresh roots, not derived from the setup context: time spent at the prompt must not
	// count against the import or verification budgets.
	private := trace.OmitBody(context.Background())
	ictx, icancel := context.WithTimeout(private, u.timeout)
	defer icancel()
	if err := c.ImportPhonebook(ictx, pb.ID, newXML, onConfirm); err != nil {
		return err
	}
	vctx, vcancel := context.WithTimeout(private, verifyTimeout)
	defer vcancel()
	after, err := c.ExportPhonebook(vctx, pb.ID, pb.Name)
	if err != nil {
		return fmt.Errorf("the import was accepted but could not be verified (export failed: %w)", err)
	}
	got, err := fbxml.Inspect(bytes.NewReader(after))
	if err != nil {
		return fmt.Errorf("the import was accepted but could not be verified: %w", err)
	}
	if got.Contacts != m {
		return fmt.Errorf("count mismatch: uploaded %d contacts but the phonebook now holds %d", m, got.Contacts)
	}
	fmt.Fprintf(u.env.Stdout, "Phonebook %q replaced: %d contacts.\n", pb.Name, m)
	return nil
}

// writeBackup creates the backup exclusively with mode 0600 and syncs it. If the name is
// taken (same second), -2, -3, ... is appended; it never overwrites an existing backup.
func writeBackup(dir, phonebook string, now time.Time, data []byte) (string, error) {
	base := fmt.Sprintf("fritzbook-backup-%s-%s", safeName(phonebook), now.Format("20060102-150405"))
	for i := 1; i <= 99; i++ {
		name := base + ".xml"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.xml", base, i)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", err
		}
		return path, nil
	}
	return "", errors.New("too many backups with the same timestamp")
}

// safeName makes a phonebook name usable in a file name.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), ".")
	if len(out) > 64 {
		out = out[:64] // ASCII only, so no rune is cut
	}
	if out == "" {
		return "phonebook"
	}
	return out
}
