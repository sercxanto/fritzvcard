package main

import (
	"fmt"
	"os"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Printf("fritzbook %s\n", version)
		return
	}
	fmt.Fprintln(os.Stderr, "usage: fritzbook version")
	os.Exit(2)
}
