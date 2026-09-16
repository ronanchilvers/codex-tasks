//go:build linux || darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// configureProcess isolates the child in a process group for signal forwarding.
func configureProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalProcess forwards a signal to the entire child process group.
func signalProcess(process *os.Process, signal os.Signal) error {
	systemSignal, ok := signal.(syscall.Signal)
	if !ok {
		return process.Signal(signal)
	}
	err := syscall.Kill(-process.Pid, systemSignal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// signalExitCode returns the conventional shell exit code for a terminating signal.
func signalExitCode(signal os.Signal) int {
	if systemSignal, ok := signal.(syscall.Signal); ok {
		return 128 + int(systemSignal)
	}
	return exitFailure
}
