package cli

import (
	"flag"
	"fmt"
	"io"
)

type commandFlags struct {
	*flag.FlagSet
	synopsis string
}

func newCommandFlags(name, synopsis string, stderr io.Writer) commandFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	// Parse prints its own diagnostics. Choose the stream for the usage page
	// once we know whether Parse returned ErrHelp or an actual error.
	fs.Usage = func() {}
	return commandFlags{fs, synopsis}
}

func (c commandFlags) writeHelp(w io.Writer) {
	fmt.Fprintf(w, "usage: fritzbook %s%s\n\noptions:\n", c.Name(), c.synopsis)
	fmt.Fprintln(w, "  -h  --help\tshow this help")
	c.VisitAll(func(f *flag.Flag) {
		prefix := "--"
		if len(f.Name) == 1 {
			prefix = "-"
		}
		arg, desc := flag.UnquoteUsage(f)
		fmt.Fprintf(w, "  %s%s", prefix, f.Name)
		if arg != "" {
			fmt.Fprintf(w, " %s", arg)
		}
		fmt.Fprintf(w, "\t%s", desc)
		if arg != "" && f.DefValue != "" {
			fmt.Fprintf(w, " (default %s)", f.DefValue)
		}
		fmt.Fprintln(w)
	})
}

func (c commandFlags) parseExit(err error, env Env) int {
	if err == flag.ErrHelp {
		c.writeHelp(env.Stdout)
		return 0
	}
	c.writeHelp(env.Stderr)
	return 2
}
