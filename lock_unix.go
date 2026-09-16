//go:build linux || darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// taskLock holds an operating-system advisory lock for one task ID.
type taskLock struct {
	file *os.File
}

// acquireTaskLock obtains a nonblocking per-task advisory lock.
func acquireTaskLock(stateDir, taskID string) (*taskLock, bool, error) {
	locksDir := filepath.Join(stateDir, "locks")
	if err := os.MkdirAll(locksDir, 0o700); err != nil {
		return nil, false, fmt.Errorf("create lock directory %q: %w", locksDir, err)
	}
	if err := os.Chmod(locksDir, 0o700); err != nil {
		return nil, false, fmt.Errorf("set lock directory permissions %q: %w", locksDir, err)
	}
	file, err := os.OpenFile(filepath.Join(locksDir, taskID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open task lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock task: %w", err)
	}
	return &taskLock{file: file}, true, nil
}

// close releases the advisory lock while retaining its on-disk lock file.
func (lock *taskLock) close() error {
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return fmt.Errorf("unlock task: %w", unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close task lock: %w", closeErr)
	}
	return nil
}
