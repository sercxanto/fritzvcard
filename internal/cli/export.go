package cli

import (
	"bytes"
	"fmt"
	"io"

	"fritzvcard/internal/contacts"
	"fritzvcard/internal/fbxml"
)

func runExport(args []string, env Env) int {
	fs := newCommandFlags("export", " [flags] <file.vcf>...", env.Stderr)
	out := fs.String("o", "-", "output file (- for stdout)")
	name := fs.String("name", "Nextcloud", "phonebook name")
	files, err := parseInterspersed(fs.FlagSet, args)
	if err != nil {
		return fs.parseExit(err, env)
	}
	if len(files) == 0 {
		fmt.Fprintln(env.Stderr, "fritzvcard export: at least one .vcf file is required")
		fs.writeHelp(env.Stderr)
		return 2
	}

	cs, skipped, err := contacts.ParseFiles(files)
	if err != nil {
		fmt.Fprintf(env.Stderr, "fritzvcard export: %v\n", err)
		return 1
	}
	var buf bytes.Buffer
	if err := fbxml.Write(&buf, *name, cs); err != nil {
		fmt.Fprintf(env.Stderr, "fritzvcard export: %v\n", err)
		return 1
	}
	if *out == "-" {
		_, err = io.Copy(env.Stdout, &buf)
	} else {
		err = writeExportFile(*out, func(w io.Writer) error {
			_, err := buf.WriteTo(w)
			return err
		})
	}
	if err != nil {
		fmt.Fprintf(env.Stderr, "fritzvcard export: %v\n", err)
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
