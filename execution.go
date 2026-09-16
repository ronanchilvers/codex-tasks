package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// runner contains process-bound dependencies used to execute one task.
type runner struct {
	stdout       io.Writer
	stderr       io.Writer
	lookPath     func(string) (string, error)
	now          func() time.Time
	stateDir     func() (string, error)
	signalSource func(chan<- os.Signal, ...os.Signal)
	signalStop   func(chan<- os.Signal)
}

// newRunner creates a runner backed by the current process environment.
func newRunner(stdout, stderr io.Writer) runner {
	return runner{
		stdout:       stdout,
		stderr:       stderr,
		lookPath:     exec.LookPath,
		now:          time.Now,
		stateDir:     localStateDir,
		signalSource: signal.Notify,
		signalStop:   signal.Stop,
	}
}

// run resolves, previews, or executes one configured task.
func (current runner) run(cfg config) int {
	selectedTask, err := loadTask(cfg)
	if err != nil {
		fmt.Fprintf(current.stderr, "codex-task: %v\n", err)
		return exitFailure
	}
	if err := validateTaskOptions(selectedTask, cfg.forwarded); err != nil {
		fmt.Fprintf(current.stderr, "codex-task: %v\n", err)
		return exitFailure
	}
	taskDir := filepath.Join(selectedTask.memoryDir, selectedTask.taskID)
	if cfg.dryRun {
		prompt, err := promptWithMemories(selectedTask.prompt, taskDir, cfg.memoryCount)
		if err != nil {
			fmt.Fprintf(current.stderr, "codex-task: %v\n", err)
			return exitFailure
		}
		selectedTask.prompt = prompt
		printDryRun(current.stdout, selectedTask, cfg.forwarded)
		return exitSuccess
	}

	codexPath, err := current.lookPath("codex")
	if err != nil {
		fmt.Fprintf(current.stderr, "codex-task: find codex executable: %v\n", err)
		return exitFailure
	}
	stateDir, err := current.stateDir()
	if err != nil {
		fmt.Fprintf(current.stderr, "codex-task: %v\n", err)
		return exitFailure
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		fmt.Fprintf(current.stderr, "codex-task: create state directory %q: %v\n", stateDir, err)
		return exitFailure
	}
	if err := os.Chmod(stateDir, 0o700); err != nil {
		fmt.Fprintf(current.stderr, "codex-task: set state directory permissions %q: %v\n", stateDir, err)
		return exitFailure
	}
	taskDir, err = prepareTaskDirectory(selectedTask.memoryDir, selectedTask.taskID)
	if err != nil {
		fmt.Fprintf(current.stderr, "codex-task: %v\n", err)
		return exitFailure
	}

	lock, acquired, err := acquireTaskLock(stateDir, selectedTask.taskID)
	if err != nil {
		fmt.Fprintf(current.stderr, "codex-task: %v\n", err)
		return exitFailure
	}
	if !acquired {
		fmt.Fprintf(current.stderr, "codex-task: task already running; skipped %s\n", selectedTask.promptPath)
		return exitLocked
	}
	defer func() {
		if err := lock.close(); err != nil {
			fmt.Fprintf(current.stderr, "codex-task: warning: %v\n", err)
		}
	}()
	prompt, err := promptWithMemories(selectedTask.prompt, taskDir, cfg.memoryCount)
	if err != nil {
		fmt.Fprintf(current.stderr, "codex-task: %v\n", err)
		return exitFailure
	}
	selectedTask.prompt = prompt

	return current.execute(codexPath, selectedTask, taskDir, cfg.forwarded)
}

// validateTaskOptions rejects duplicate Codex settings from task metadata and forwarded arguments.
func validateTaskOptions(selectedTask task, forwarded []string) error {
	for index, argument := range forwarded {
		if selectedTask.model != "" && isModelOption(argument) {
			return errors.New("front matter model conflicts with forwarded --model; remove one")
		}
		if selectedTask.effort == "" {
			continue
		}
		if configValue, found := forwardedConfigValue(forwarded, index, argument); found && isReasoningEffortConfig(configValue) {
			return errors.New("front matter effort conflicts with forwarded model_reasoning_effort configuration; remove one")
		}
	}
	return nil
}

// isModelOption reports whether argument selects a Codex model.
func isModelOption(argument string) bool {
	return argument == "--model" || strings.HasPrefix(argument, "--model=") || argument == "-m" || strings.HasPrefix(argument, "-m=") || strings.HasPrefix(argument, "-m") && len(argument) > len("-m")
}

// forwardedConfigValue returns the value supplied by one forwarded config option.
func forwardedConfigValue(arguments []string, index int, argument string) (string, bool) {
	if argument == "--config" || argument == "-c" {
		if index+1 < len(arguments) {
			return arguments[index+1], true
		}
		return "", false
	}
	if value, found := strings.CutPrefix(argument, "--config="); found {
		return value, true
	}
	if value, found := strings.CutPrefix(argument, "-c="); found {
		return value, true
	}
	if strings.HasPrefix(argument, "-c") && len(argument) > len("-c") {
		return argument[len("-c"):], true
	}
	return "", false
}

// isReasoningEffortConfig reports whether configValue sets Codex's reasoning effort key.
func isReasoningEffortConfig(configValue string) bool {
	key, _, found := strings.Cut(configValue, "=")
	return found && strings.TrimSpace(key) == "model_reasoning_effort"
}

// execute invokes Codex, captures its final response, and persists an appropriate record.
func (current runner) execute(codexPath string, selectedTask task, taskDir string, forwarded []string) int {
	capture, err := os.CreateTemp(taskDir, ".capture-*.tmp")
	if err != nil {
		fmt.Fprintf(current.stderr, "codex-task: create temporary output: %v\n", err)
		return exitFailure
	}
	capturePath := capture.Name()
	if err := capture.Chmod(0o600); err != nil {
		_ = capture.Close()
		_ = os.Remove(capturePath)
		fmt.Fprintf(current.stderr, "codex-task: set temporary output permissions: %v\n", err)
		return exitFailure
	}
	if err := capture.Close(); err != nil {
		_ = os.Remove(capturePath)
		fmt.Fprintf(current.stderr, "codex-task: close temporary output: %v\n", err)
		return exitFailure
	}
	removeCapture := true
	defer func() {
		if removeCapture {
			_ = os.Remove(capturePath)
		}
	}()

	started := current.now().UTC()
	command := exec.Command(codexPath, commandArguments(selectedTask, forwarded, capturePath)...)
	command.Dir = selectedTask.workDir
	command.Env = os.Environ()
	command.Stdin = bytes.NewReader(selectedTask.prompt)
	command.Stdout = current.stdout
	command.Stderr = current.stderr
	configureProcess(command)

	signals := make(chan os.Signal, 1)
	current.signalSource(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	if err := command.Start(); err != nil {
		current.signalStop(signals)
		fmt.Fprintf(current.stderr, "codex-task: start codex: %v\n", err)
		return exitFailure
	}
	interrupted := current.forwardSignals(command, signals)
	waitErr := command.Wait()
	close(interrupted.done)
	current.signalStop(interrupted.channel)
	ended := current.now().UTC()

	response, readErr := os.ReadFile(capturePath)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		removeCapture = false
		fmt.Fprintf(current.stderr, "codex-task: read captured output: %v; recoverable output: %s\n", readErr, capturePath)
		return exitFailure
	}
	exitCode, status, failed := childResult(waitErr, interrupted.signal())
	if waitErr == nil && len(bytes.TrimSpace(response)) == 0 {
		fmt.Fprintln(current.stderr, "codex-task: codex succeeded but produced no final response")
		return exitFailure
	}
	if failed && len(bytes.TrimSpace(response)) == 0 {
		fmt.Fprintf(current.stderr, "codex-task: %s; no final response was available to save\n", status)
		return exitCode
	}

	recordPath, err := saveMemory(taskDir, memoryRecord{
		promptPath: selectedTask.promptPath,
		started:    started,
		ended:      ended,
		status:     status,
		response:   response,
		failed:     failed,
	})
	if err != nil {
		removeCapture = false
		fmt.Fprintf(current.stderr, "codex-task: save memory: %v; recoverable output: %s\n", err, capturePath)
		return exitFailure
	}
	fmt.Fprintf(current.stderr, "codex-task: saved record: %s\n", recordPath)
	if failed {
		fmt.Fprintf(current.stderr, "codex-task: %s\n", status)
	}
	return exitCode
}

// forwardedSignals tracks signals delivered while a child is active.
type forwardedSignals struct {
	channel chan os.Signal
	done    chan struct{}
	seen    chan os.Signal
}

// forwardSignals relays registered termination signals to the child's process group until it exits.
func (current runner) forwardSignals(command *exec.Cmd, signals chan os.Signal) forwardedSignals {
	done := make(chan struct{})
	seen := make(chan os.Signal, 1)
	go func() {
		for {
			select {
			case received := <-signals:
				select {
				case seen <- received:
				default:
				}
				if err := signalProcess(command.Process, received); err != nil {
					fmt.Fprintf(current.stderr, "codex-task: forward signal: %v\n", err)
				}
			case <-done:
				return
			}
		}
	}()
	return forwardedSignals{channel: signals, done: done, seen: seen}
}

// signal returns the first signal forwarded to the child, if any.
func (signals forwardedSignals) signal() os.Signal {
	select {
	case received := <-signals.seen:
		return received
	default:
		return nil
	}
}

// childResult describes a completed child and chooses the wrapper's exit code.
func childResult(waitErr error, interrupted os.Signal) (int, string, bool) {
	if interrupted != nil {
		return signalExitCode(interrupted), fmt.Sprintf("interrupted by %s", interrupted), true
	}
	if waitErr == nil {
		return exitSuccess, "success", false
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		code := exitErr.ExitCode()
		if code > 0 {
			return code, fmt.Sprintf("failed (codex exit code %d)", code), true
		}
	}
	return exitFailure, fmt.Sprintf("failed (codex process: %s)", strings.TrimSpace(waitErr.Error())), true
}
