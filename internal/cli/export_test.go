package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fritzbook/internal/fbxml"
)

const fixture = "../../testdata/vcf/nextcloud-v3.vcf"

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, Env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
		Getenv: func(string) string { return "" }})
	return code, out.String(), errb.String()
}

func checkOut(t *testing.T, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := fbxml.Inspect(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "X" || info.Contacts < 1 {
		t.Fatalf("unexpected info %+v", info)
	}
}

func TestExport_WritesFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.xml")
	code, _, stderr := run(t, "export", fixture, "-o", out, "--name", "X")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.HasPrefix(stderr, "Converted") {
		t.Fatalf("stderr: %q", stderr)
	}
	checkOut(t, out)
}

func TestExport_FlagsBeforeAndAfterFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.xml")
	code, _, stderr := run(t, "export", "-o", out, fixture, "--name", "X")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	checkOut(t, out)
}

func TestExport_Stdout(t *testing.T) {
	code, stdout, _ := run(t, "export", fixture)
	if code != 0 || !strings.Contains(stdout, "<phonebook") {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}

func TestExport_NoFiles(t *testing.T) {
	if code, _, _ := run(t, "export"); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestExport_MissingFile(t *testing.T) {
	code, _, stderr := run(t, "export", "/nonexistent/x.vcf")
	if code != 1 || !strings.Contains(stderr, "/nonexistent/x.vcf") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestVersion(t *testing.T) {
	Version = "1.2.3"
	code, stdout, _ := run(t, "version")
	if code != 0 || stdout != "fritzbook 1.2.3\n" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}
