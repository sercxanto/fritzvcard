package cli

import (
	"errors"
	"fmt"
)

// password returns the password from FRITZBOX_PASSWORD or, on a terminal, by prompting.
// It never echoes or logs the value.
func password(env Env, user string) (string, error) {
	if p := env.Getenv("FRITZBOX_PASSWORD"); p != "" {
		return p, nil
	}
	if env.ReadPassword == nil {
		return "", errors.New("no terminal: set FRITZBOX_PASSWORD")
	}
	fmt.Fprintf(env.Stderr, "Password for %s: ", user)
	p, err := env.ReadPassword()
	fmt.Fprintln(env.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if p == "" {
		return "", errors.New("empty password")
	}
	return p, nil
}
