package main

import (
	"fmt"
	"io"
	"os"
)

const usage = `herdr-apps is the Herdr application discovery plugin.

Usage:
  herdr-apps daemon
  herdr-apps doctor
  herdr-apps help

The daemon owns application state. Live Herdr validation is required before release.
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		_, err := fmt.Fprint(out, usage)
		return err
	}

	return fmt.Errorf("%q is not available in the foundation release; run %q for usage", args[0], "herdr-apps help")
}
