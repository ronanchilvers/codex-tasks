package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestParseArguments verifies wrapper parsing and lossless forwarding boundaries.
func TestParseArguments(t *testing.T) {
	var output bytes.Buffer
	cfg, help, err := parseArguments([]string{
		"--prompt", "prompt with spaces.md", "--memory-dir=memory", "--dry-run", "--",
		"--model", "a model", "--config", `value='quoted;$(ignored)'`,
	}, &output)
	if err != nil || help {
		t.Fatalf("parseArguments() = help %v, error %v", help, err)
	}
	if cfg.promptPath != "prompt with spaces.md" || cfg.memoryDir != "memory" || !cfg.dryRun {
		t.Fatalf("unexpected config: %#v", cfg)
	}
	want := []string{"--model", "a model", "--config", `value='quoted;$(ignored)'`}
	if strings.Join(cfg.forwarded, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("forwarded arguments = %#v, want %#v", cfg.forwarded, want)
	}
}

// TestParseArgumentsUsageErrors verifies required, unknown, positional, and conflicting inputs.
func TestParseArgumentsUsageErrors(t *testing.T) {
	tests := [][]string{
		{},
		{"--unknown"},
		{"--prompt", "one.md", "extra"},
		{"--prompt", "one.md", "--", "-o", "out"},
		{"--prompt", "one.md", "--", "-o=out"},
		{"--prompt", "one.md", "--", "--output-last-message"},
		{"--prompt", "one.md", "--", "--output-last-message=out"},
		{"--prompt", "one.md", "--", "--"},
	}
	for _, args := range tests {
		if _, _, err := parseArguments(args, &bytes.Buffer{}); err == nil {
			t.Errorf("parseArguments(%q) unexpectedly succeeded", args)
		}
	}
}

// TestHelp verifies help exits successfully and explains forwarding.
func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("run(--help) exit = %d, want %d", code, exitSuccess)
	}
	if !strings.Contains(stderr.String(), "Arguments after --") {
		t.Fatalf("help did not explain forwarding:\n%s", stderr.String())
	}
}
