package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
	exitLocked  = 75
)

// config contains the validated wrapper arguments and opaque Codex arguments.
type config struct {
	promptPath string
	memoryDir  string
	dryRun     bool
	forwarded  []string
}

// parseArguments separates wrapper flags from Codex arguments and validates ownership conflicts.
func parseArguments(args []string, output io.Writer) (config, bool, error) {
	wrapperArgs, forwarded := splitForwarded(args)
	var cfg config
	flags := flag.NewFlagSet("codex-task", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.promptPath, "prompt", "", "path to a nonempty Markdown prompt file (required)")
	flags.StringVar(&cfg.memoryDir, "memory-dir", "", "directory in which to store Markdown run records")
	flags.BoolVar(&cfg.dryRun, "dry-run", false, "preview the resolved task without executing it")
	flags.Usage = func() { printUsage(output, flags) }

	if err := flags.Parse(wrapperArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return config{}, true, nil
		}
		flags.Usage()
		return config{}, false, err
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return config{}, false, fmt.Errorf("unexpected positional argument %q; put Codex options after --", flags.Arg(0))
	}
	if cfg.promptPath == "" {
		flags.Usage()
		return config{}, false, errors.New("--prompt is required")
	}
	if err := validateForwarded(forwarded); err != nil {
		flags.Usage()
		return config{}, false, err
	}
	cfg.forwarded = append([]string(nil), forwarded...)
	return cfg, false, nil
}

// splitForwarded splits arguments at the first standalone double dash.
func splitForwarded(args []string) ([]string, []string) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// validateForwarded rejects arguments that interfere with wrapper-owned input or output.
func validateForwarded(args []string) error {
	for _, arg := range args {
		if arg == "-o" || strings.HasPrefix(arg, "-o=") || arg == "--output-last-message" || strings.HasPrefix(arg, "--output-last-message=") {
			return errors.New("forwarded -o/--output-last-message conflicts with wrapper-managed output capture")
		}
		if arg == "--" {
			return errors.New("a forwarded -- separator is unsupported because the prompt is supplied by --prompt")
		}
	}
	return nil
}

// printUsage writes command syntax and the wrapper's execution contract.
func printUsage(output io.Writer, flags *flag.FlagSet) {
	fmt.Fprintln(output, "Usage: codex-task --prompt PATH [--memory-dir PATH] [--dry-run] [-- CODEX_OPTIONS...]")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Runs one prompt through `codex exec`, skips overlapping runs of the same")
	fmt.Fprintln(output, "front-matter task_id, and saves the final response as Markdown.")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Wrapper flags:")
	flags.PrintDefaults()
	fmt.Fprintln(output)
	fmt.Fprintln(output, "Arguments after -- are passed directly to `codex exec`. The wrapper owns")
	fmt.Fprintln(output, "standard input and -o/--output-last-message. Exit codes: 0 success, 1 wrapper")
	fmt.Fprintln(output, "failure, 2 usage error, 75 already running; Codex nonzero codes are propagated.")
}
