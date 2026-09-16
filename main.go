package main

import (
	"fmt"
	"io"
	"os"
)

// main parses the command line, runs one task, and maps its result to an exit code.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes the command-line application without terminating the calling process.
func run(args []string, stdout, stderr io.Writer) int {
	config, help, err := parseArguments(args, stderr)
	if help {
		return exitSuccess
	}
	if err != nil {
		fmt.Fprintf(stderr, "codex-task: %v\n", err)
		return exitUsage
	}

	runner := newRunner(stdout, stderr)
	return runner.run(config)
}
