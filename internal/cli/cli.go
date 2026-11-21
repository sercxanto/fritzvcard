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
  export <file.vcf>...   convert vCard files to FRITZ!Box phonebook XML
  version                print the version
`

// Run executes the command in args and returns the exit code:
// 0 ok, 1 runtime error, 2 usage error.
func Run(args []string, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "export":
		return runExport(args[1:], env)
	case "version":
		fmt.Fprintf(env.Stdout, "fritzbook %s\n", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(env.Stdout, usage)
		return 0
	default:
		fmt.Fprintf(env.Stderr, "fritzbook: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
