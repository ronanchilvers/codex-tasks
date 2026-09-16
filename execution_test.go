package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeCodex creates an executable that captures its invocation and optionally writes a response.
func fakeCodex(t *testing.T, response string, exitCode int, sleepSeconds int) (string, string, string, string) {
	t.Helper()
	directory := t.TempDir()
	scriptPath := filepath.Join(directory, "fake-codex")
	argsPath := filepath.Join(directory, "arguments")
	stdinPath := filepath.Join(directory, "stdin")
	cwdPath := filepath.Join(directory, "cwd")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %s
pwd > %s
cat > %s
output=''
take_output='no'
for argument in "$@"; do
    if [ "$take_output" = 'yes' ]; then
        output="$argument"
        take_output='no'
    elif [ "$argument" = '--output-last-message' ]; then
        take_output='yes'
    fi
done
printf 'fake stdout\n'
printf 'fake stderr\n' >&2
%s
%s
exit %d
`, quoteArgument(argsPath), quoteArgument(cwdPath), quoteArgument(stdinPath), shellResponse(response), shellSleep(sleepSeconds), exitCode)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return scriptPath, argsPath, stdinPath, cwdPath
}

// shellSleep renders an optional fixed sleep command for a fake executable.
func shellSleep(seconds int) string {
	if seconds == 0 {
		return ":"
	}
	return fmt.Sprintf("sleep %d", seconds)
}

// shellResponse renders a safe response write for a fake executable.
func shellResponse(response string) string {
	if response == "" {
		return ":"
	}
	return "printf '%s' " + quoteArgument(response) + " > \"$output\""
}

// testRunner creates an isolated runner with deterministic timestamps.
func testRunner(stdout, stderr *bytes.Buffer, executable, stateDir string) runner {
	return runner{
		stdout:   stdout,
		stderr:   stderr,
		lookPath: func(string) (string, error) { return executable, nil },
		now:      func() time.Time { return time.Date(2026, 9, 15, 12, 30, 0, 123, time.FixedZone("test", 3600)) },
		stateDir: func() (string, error) { return stateDir, nil },
		signalSource: func(chan<- os.Signal, ...os.Signal) {
		},
		signalStop: func(chan<- os.Signal) {},
	}
}

// testConfig creates a prompt and returns a configuration for an isolated run.
func testConfig(t *testing.T, prompt []byte) config {
	return testConfigWithFrontMatter(t, "task_id: test-task", prompt)
}

// testConfigWithFrontMatter creates a prompt with task front matter and returns an isolated run configuration.
func testConfigWithFrontMatter(t *testing.T, frontMatter string, prompt []byte) config {
	t.Helper()
	directory := t.TempDir()
	promptPath := filepath.Join(directory, "prompt.md")
	prompt = append([]byte("---\n"+frontMatter+"\n---\n"), prompt...)
	if err := os.WriteFile(promptPath, prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	return config{promptPath: promptPath, memoryDir: filepath.Join(directory, "memories")}
}

// TestExecuteAppliesFrontMatterModelOptions verifies prompt metadata controls Codex model settings.
func TestExecuteAppliesFrontMatterModelOptions(t *testing.T) {
	executable, argsPath, _, _ := fakeCodex(t, "final response", 0, 0)
	cfg := testConfigWithFrontMatter(t, "model: gpt-5.6-luna\neffort: high\ntask_id: test-task", []byte("prompt"))
	cfg.forwarded = []string{"--sandbox", "workspace-write"}
	var stdout, stderr bytes.Buffer
	current := testRunner(&stdout, &stderr, executable, filepath.Join(t.TempDir(), "state"))
	if code := current.run(cfg); code != exitSuccess {
		t.Fatalf("runner exit = %d; stderr:\n%s", code, stderr.String())
	}
	arguments, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"exec", "--sandbox", "workspace-write", "--model", "gpt-5.6-luna",
		"--config", `model_reasoning_effort="high"`, "--output-last-message",
	}
	argumentLines := strings.Split(strings.TrimSuffix(string(arguments), "\n"), "\n")
	if len(argumentLines) != len(want)+2 || strings.Join(argumentLines[:len(want)], "\x00") != strings.Join(want, "\x00") || argumentLines[len(argumentLines)-1] != "-" {
		t.Fatalf("arguments = %#v, want prefix %#v followed by output path and -", argumentLines, want)
	}
}

// TestExecuteRejectsConflictingFrontMatterModelOptions verifies duplicate Codex settings fail before execution.
func TestExecuteRejectsConflictingFrontMatterModelOptions(t *testing.T) {
	executable, _, _, _ := fakeCodex(t, "final response", 0, 0)
	for name, forwarded := range map[string][]string{
		"model":  {"--model", "forwarded-model"},
		"effort": {"--config=model_reasoning_effort=low"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfigWithFrontMatter(t, "model: gpt-5.6-luna\neffort: high\ntask_id: test-task", []byte("prompt"))
			cfg.forwarded = forwarded
			var stdout, stderr bytes.Buffer
			current := testRunner(&stdout, &stderr, executable, filepath.Join(t.TempDir(), "state"))
			if code := current.run(cfg); code != exitFailure {
				t.Fatalf("runner exit = %d, want %d", code, exitFailure)
			}
			if !strings.Contains(stderr.String(), "front matter") {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

// TestExecuteSuccess verifies exact process input, streaming, capture, and persisted metadata.
func TestExecuteSuccess(t *testing.T) {
	executable, argsPath, stdinPath, cwdPath := fakeCodex(t, "final response\n", 0, 0)
	cfg := testConfig(t, []byte("prompt bytes\n"))
	cfg.forwarded = []string{"--model", "model with spaces", "--config", `x=';$(literal)'`}
	var stdout, stderr bytes.Buffer
	current := testRunner(&stdout, &stderr, executable, filepath.Join(t.TempDir(), "state"))
	if code := current.run(cfg); code != exitSuccess {
		t.Fatalf("runner exit = %d; stderr:\n%s", code, stderr.String())
	}
	if stdout.String() != "fake stdout\n" || !strings.Contains(stderr.String(), "fake stderr") {
		t.Fatalf("streams were not inherited: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != "prompt bytes\n" {
		t.Fatalf("stdin = %q", stdin)
	}
	arguments, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	argumentLines := strings.Split(strings.TrimSuffix(string(arguments), "\n"), "\n")
	if len(argumentLines) != 8 || argumentLines[0] != "exec" || argumentLines[1] != "--model" || argumentLines[2] != "model with spaces" || argumentLines[3] != "--config" || argumentLines[4] != `x=';$(literal)'` || argumentLines[5] != "--output-last-message" || argumentLines[7] != "-" {
		t.Fatalf("unexpected arguments: %#v", argumentLines)
	}
	wantDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.ReadFile(cwdPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(cwd)) != wantDirectory {
		t.Fatalf("child cwd = %q, want %q", strings.TrimSpace(string(cwd)), wantDirectory)
	}
	recordPath := savedPath(stderr.String())
	if filepath.Base(filepath.Dir(recordPath)) != "test-task" {
		t.Fatalf("record task directory = %q", filepath.Dir(recordPath))
	}
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Status: success", "2026-09-15T11:30:00.000000123Z", "final response\n"} {
		if !strings.Contains(string(record), expected) {
			t.Errorf("record missing %q:\n%s", expected, record)
		}
	}
	info, err := os.Stat(recordPath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %v, error %v", info.Mode().Perm(), err)
	}
}

// savedPath extracts the saved record path from standard-error diagnostics.
func savedPath(stderr string) string {
	const marker = "codex-task: saved record: "
	start := strings.LastIndex(stderr, marker)
	if start < 0 {
		return ""
	}
	remainder := stderr[start+len(marker):]
	if end := strings.IndexByte(remainder, '\n'); end >= 0 {
		remainder = remainder[:end]
	}
	return strings.TrimSpace(remainder)
}

// TestExecuteFailureArtifact verifies child exit propagation and failed-record naming.
func TestExecuteFailureArtifact(t *testing.T) {
	executable, _, _, _ := fakeCodex(t, "partial result", 42, 0)
	cfg := testConfig(t, []byte("prompt"))
	var stdout, stderr bytes.Buffer
	current := testRunner(&stdout, &stderr, executable, filepath.Join(t.TempDir(), "state"))
	if code := current.run(cfg); code != 42 {
		t.Fatalf("runner exit = %d, want 42; stderr:\n%s", code, stderr.String())
	}
	path := savedPath(stderr.String())
	if !strings.HasPrefix(filepath.Base(path), "failed-") {
		t.Fatalf("failed record path = %q", path)
	}
	record, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "Status: failed (codex exit code 42)") || !strings.HasSuffix(string(record), "partial result\n") {
		t.Fatalf("unexpected failed record:\n%s", record)
	}
}

// TestExecuteMissingOutput verifies that a successful child without final output fails.
func TestExecuteMissingOutput(t *testing.T) {
	executable, _, _, _ := fakeCodex(t, "", 0, 0)
	cfg := testConfig(t, []byte("prompt"))
	var stdout, stderr bytes.Buffer
	current := testRunner(&stdout, &stderr, executable, filepath.Join(t.TempDir(), "state"))
	if code := current.run(cfg); code != exitFailure {
		t.Fatalf("runner exit = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), "produced no final response") || savedPath(stderr.String()) != "" {
		t.Fatalf("unexpected diagnostics:\n%s", stderr.String())
	}
}

// TestExecuteForwardsTermination verifies interruption status and process-group cleanup.
func TestExecuteForwardsTermination(t *testing.T) {
	executable, _, _, _ := fakeCodex(t, "response before interruption", 0, 10)
	cfg := testConfig(t, []byte("prompt"))
	var stdout, stderr bytes.Buffer
	current := testRunner(&stdout, &stderr, executable, filepath.Join(t.TempDir(), "state"))
	current.signalSource = func(signals chan<- os.Signal, _ ...os.Signal) {
		go func() {
			time.Sleep(500 * time.Millisecond)
			signals <- syscall.SIGTERM
		}()
	}
	started := time.Now()
	if code := current.run(cfg); code != 128+int(syscall.SIGTERM) {
		t.Fatalf("interrupted runner exit = %d; stderr:\n%s", code, stderr.String())
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("child process group did not terminate promptly")
	}
	recordPath := savedPath(stderr.String())
	if !strings.HasPrefix(filepath.Base(recordPath), "failed-") {
		t.Fatalf("interrupted record path = %q", recordPath)
	}
	record, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "Status: interrupted by terminated") {
		t.Fatalf("unexpected interrupted record:\n%s", record)
	}
}

// TestTaskLockContentionAndRelease verifies aliases contend immediately and later runs recover.
func TestTaskLockContentionAndRelease(t *testing.T) {
	executable, argsPath, _, _ := fakeCodex(t, "complete", 0, 1)
	cfg := testConfig(t, []byte("prompt"))
	alias := filepath.Join(filepath.Dir(cfg.promptPath), "alias.md")
	if err := os.Symlink(cfg.promptPath, alias); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	firstDone := make(chan int, 1)
	var firstOut, firstErr bytes.Buffer
	go func() {
		firstDone <- testRunner(&firstOut, &firstErr, executable, stateDir).run(cfg)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(argsPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first fake Codex process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	aliasConfig := cfg
	aliasConfig.promptPath = alias
	aliasConfig.memoryDir = filepath.Join(t.TempDir(), "alternate-memory")
	var secondOut, secondErr bytes.Buffer
	if code := testRunner(&secondOut, &secondErr, executable, stateDir).run(aliasConfig); code != exitLocked {
		t.Fatalf("contending runner exit = %d, want %d; stderr:\n%s", code, exitLocked, secondErr.String())
	}
	if code := <-firstDone; code != exitSuccess {
		t.Fatalf("first runner exit = %d; stderr:\n%s", code, firstErr.String())
	}
	var thirdOut, thirdErr bytes.Buffer
	if code := testRunner(&thirdOut, &thirdErr, executable, stateDir).run(aliasConfig); code != exitSuccess {
		t.Fatalf("later runner exit = %d; stderr:\n%s", code, thirdErr.String())
	}
}
