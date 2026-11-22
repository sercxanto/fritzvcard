// Package cli implements the fritzbook command-line interface.
package cli

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// Version is the build version, set by main.
var Version = "dev"

// Env carries the process environment so commands are testable.
type Env struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	Getenv       func(string) string
	ReadPassword func() (string, error) // nil when stdin is not a terminal
	Now          func() time.Time
	HTTPClient   *http.Client // nil means default; tests inject a fake transport
}

const usage = `usage: fritzbook <command> [arguments]

commands:
  export   convert vCard files to FRITZ!Box phonebook XML
  upload   replace a named FRITZ!Box phonebook (with backup)
  version  print the version

For command options, use either order: fritzbook upload --help or fritzbook --help upload.
`

// Run executes the command in args and returns the exit code:
// 0 ok, 1 runtime error, 2 usage error.
func Run(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "help", "-h", "--help":
		switch len(args) {
		case 1:
			fmt.Fprint(env.Stdout, usage)
			return 0
		case 2:
			args = []string{args[1], "--help"}
		default:
			fmt.Fprint(env.Stderr, usage)
			return 2
		}
	}
	switch args[0] {
	case "export":
		return runExport(args[1:], env)
	case "upload":
		return runUpload(args[1:], env)
	case "version":
		fs := newCommandFlags("version", "", env.Stderr)
		rest, err := parseInterspersed(fs.FlagSet, args[1:])
		if err != nil {
			return fs.parseExit(err, env)
		}
		if len(rest) != 0 {
			fs.writeHelp(env.Stderr)
			return 2
		}
		fmt.Fprintf(env.Stdout, "fritzbook %s\n", Version)
		return 0
	default:
		fmt.Fprintf(env.Stderr, "fritzbook: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
