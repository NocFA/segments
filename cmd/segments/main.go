package main

import (
	"fmt"
	"os"

	"codeberg.org/nocfa/segments/internal/cli"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: sg <command>")
		fmt.Println("  start, stop, list, add, done, rename, setup, init, shell, uninstall, version")
		os.Exit(1)
	}

	if err := cli.Run(os.Args, version); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
