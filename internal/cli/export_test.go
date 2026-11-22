package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

func TestExport_PrivateReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := run(t, "export", fixture, "-o", path, "--name", "X"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	checkOut(t, path)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode = %o, want 600", got)
		}
	}
}

func TestWriteExportFile_FailurePreservesTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.xml")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeErr := errors.New("synthetic write failure")
	err := writeExportFile(path, func(w io.Writer) error {
		if _, err := io.WriteString(w, "partial"); err != nil {
			return err
		}
		return writeErr
	})
	if !errors.Is(err, writeErr) {
		t.Fatalf("got %v, want %v", err, writeErr)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("original file: %q, %v", data, err)
	}
	checkOnlyEntry(t, dir, "out.xml", false)
}

func TestWriteExportFile_FailedRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeExportFile(path, func(w io.Writer) error {
		_, err := io.WriteString(w, "complete")
		return err
	}); err == nil {
		t.Fatal("replacing a directory succeeded")
	}
	checkOnlyEntry(t, dir, "target", true)
}

func checkOnlyEntry(t *testing.T, dir, name string, isDir bool) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name || entries[0].IsDir() != isDir {
		t.Fatalf("unexpected directory contents: %v", entries)
	}
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
