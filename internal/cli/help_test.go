package cli

import (
	"os"
	"slices"
	"strings"
	"testing"

	"fritzbook/internal/fbxml"
)

func TestHelpForms(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		code, out, stderr := run(t, arg)
		if code != 0 || stderr != "" || !strings.Contains(out, "fritzbook upload --help") || !strings.Contains(out, "fritzbook --help upload") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", arg, code, out, stderr)
		}
	}
	for _, tc := range []struct{ cmd, flags string }{
		{"export", "-o --name"},
		{"upload", "--phonebook --host --user --yes --backup-dir --timeout --insecure --verbose"},
		{"version", ""},
	} {
		t.Run(tc.cmd, func(t *testing.T) {
			var page string
			for i, args := range [][]string{{tc.cmd, "--help"}, {"--help", tc.cmd}, {tc.cmd, "-h"}} {
				code, out, stderr := run(t, args...)
				if code != 0 || stderr != "" || !strings.HasPrefix(out, "usage: fritzbook "+tc.cmd) || strings.Count(out, "usage:") != 1 {
					t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, out, stderr)
				}
				if i > 0 && out != page {
					t.Fatalf("%v differs from command --help: %q", args, out)
				}
				page = out
			}
			tokens := strings.Fields(page)
			for _, flag := range strings.Fields(tc.flags + " -h --help") {
				wrong := "-" + flag
				if strings.HasPrefix(flag, "--") {
					wrong = flag[1:]
				}
				if !slices.Contains(tokens, flag) || slices.Contains(tokens, wrong) {
					t.Errorf("expected %s, not %s in %q", flag, wrong, page)
				}
			}
		})
	}
	for _, args := range [][]string{{"--help", "unknown"}, {"export", "--unknown"}} {
		code, out, stderr := run(t, args...)
		if code != 2 || out != "" || !strings.Contains(stderr, "unknown") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out, stderr)
		}
	}
}

func TestHelpRespectsArguments(t *testing.T) {
	for _, arg := range []string{"--name", "-name"} {
		code, out, stderr := run(t, "export", arg, "--help", fixture)
		info, err := fbxml.Inspect(strings.NewReader(out))
		if code != 0 || err != nil || info.Name != "--help" {
			t.Fatalf("%s: exit %d, stderr %q, XML %q: %v", arg, code, stderr, out, err)
		}
	}
	vcf, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	if err := os.WriteFile("--help", vcf, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr := run(t, "export", "--", "--help")
	info, err := fbxml.Inspect(strings.NewReader(out))
	if code != 0 || err != nil || info.Contacts == 0 {
		t.Fatalf("exit %d, stderr %q, XML %q: %v", code, stderr, out, err)
	}
}
