package main

import (
	"os"
	"time"

	"golang.org/x/term"

	"fritzbook/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cli.Version = version
	env := cli.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		Now:    time.Now,
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		env.ReadPassword = func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			return string(b), err
		}
	}
	os.Exit(cli.Run(os.Args[1:], env))
}
