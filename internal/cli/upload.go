package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fritzbook/internal/contacts"
	"fritzbook/internal/fbxml"
	"fritzbook/internal/fritz"
	"fritzbook/internal/trace"
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
		defer func() {
			fmt.Fprintln(env.Stderr, "--- trace (anonymized) ---")
			if _, err := rec.WriteTo(env.Stderr); err != nil {
				fmt.Fprintf(env.Stderr, "trace not written: %v\n", err)
			}
		}()
	}
	u.httpClient, rec = buildHTTPClient(env.HTTPClient, *insecure, *timeout, u.anon)
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
func buildHTTPClient(injected *http.Client, insecure bool, timeout time.Duration, anon *trace.Anonymizer) (*http.Client, *trace.Recorder) {
	var hc http.Client
	if injected != nil {
		hc = *injected
	} else {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		if insecure {
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opt-in via --insecure
		}
		hc = http.Client{Transport: tr, Timeout: timeout}
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
	cs, _, err := contacts.ParseFiles(files)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return errors.New("no contacts found in the given files; nothing to upload")
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), u.timeout)
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
	private := trace.OmitBody(ctx)
	before, err := c.ExportPhonebook(private, pb.ID, pb.Name)
	if err != nil {
		return fmt.Errorf("export of the current phonebook: %w", err)
	}
	cur, err := fbxml.Inspect(bytes.NewReader(before))
	if err != nil {
		return err
	}
	now := u.env.Now()
	path := filepath.Join(u.backupDir, fmt.Sprintf("fritzbook-backup-%s-%s.xml", safeName(pb.Name), now.Format("20060102-150405")))
	if err := os.WriteFile(path, before, 0o600); err != nil {
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
	if err := c.ImportPhonebook(private, pb.ID, newXML, onConfirm); err != nil {
		return err
	}
	after, err := c.ExportPhonebook(private, pb.ID, pb.Name)
	if err != nil {
		return fmt.Errorf("verification export: %w", err)
	}
	got, err := fbxml.Inspect(bytes.NewReader(after))
	if err != nil {
		return err
	}
	if got.Contacts != m {
		return fmt.Errorf("count mismatch: uploaded %d contacts but the phonebook now holds %d", m, got.Contacts)
	}
	fmt.Fprintf(u.env.Stdout, "Phonebook %q replaced: %d contacts.\n", pb.Name, m)
	return nil
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
