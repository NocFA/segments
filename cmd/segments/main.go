package main

import (
	"fmt"
	"os"

	"codeberg.org/nocfa/segments/internal/cli"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: segments <command>")
		fmt.Println("  start, stop, list, add, done, rename, setup, shell, uninstall")
		os.Exit(1)
	}

	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}