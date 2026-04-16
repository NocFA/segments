package main

import (
	"fmt"
	"os"

	"codeberg.org/nocfa/segments/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.Run(os.Args, version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
