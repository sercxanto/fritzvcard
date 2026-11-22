package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"fritzbook/internal/contacts"
	"fritzbook/internal/fbxml"
)

func runExport(args []string, env Env) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	out := fs.String("o", "-", "output file (- for stdout)")
	name := fs.String("name", "Nextcloud", "phonebook name")
	fs.Usage = func() {
		fmt.Fprintln(env.Stderr, "usage: fritzbook export [-o file] [--name name] <file.vcf>...")
		fs.PrintDefaults()
	}
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	if len(files) == 0 {
		fmt.Fprintln(env.Stderr, "fritzbook export: at least one .vcf file is required")
		fs.Usage()
		return 2
	}

	cs, skipped, err := contacts.ParseFiles(files)
	if err != nil {
		fmt.Fprintf(env.Stderr, "fritzbook export: %v\n", err)
		return 1
	}
	var buf bytes.Buffer
	if err := fbxml.Write(&buf, *name, cs); err != nil {
		fmt.Fprintf(env.Stderr, "fritzbook export: %v\n", err)
		return 1
	}
	if *out == "-" {
		_, err = io.Copy(env.Stdout, &buf)
	} else {
		err = os.WriteFile(*out, buf.Bytes(), 0o644)
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "fritzbook export: %v\n", err)
		return 1
	}

	printSummary(env.Stderr, cs, skipped)
	return 0
}

// printSummary writes the conversion summary shared by export and upload.
func printSummary(w io.Writer, cs []contacts.Contact, skipped []contacts.Skipped) {
	numbers := 0
	for _, c := range cs {
		numbers += len(c.Numbers)
	}
	fmt.Fprintf(w, "Converted %d contacts (%d numbers).\n", len(cs), numbers)
	if len(skipped) > 0 {
		fmt.Fprintf(w, "Skipped %d:\n", len(skipped))
		for _, s := range skipped {
			fmt.Fprintf(w, "  - %s: %s\n", s.Name, s.Reason)
		}
	}
}
