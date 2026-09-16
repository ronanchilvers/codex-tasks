package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// task is a prompt resolved to its stable identity and output destination.
type task struct {
	promptPath string
	prompt     []byte
	taskID     string
	memoryDir  string
	workDir    string
}

// loadTask resolves paths and reads and validates the prompt exactly once.
func loadTask(cfg config) (task, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return task{}, fmt.Errorf("get working directory: %w", err)
	}
	absPrompt, err := filepath.Abs(cfg.promptPath)
	if err != nil {
		return task{}, fmt.Errorf("resolve prompt path: %w", err)
	}
	canonicalPrompt, err := filepath.EvalSymlinks(absPrompt)
	if err != nil {
		return task{}, fmt.Errorf("resolve prompt path %q: %w", absPrompt, err)
	}
	info, err := os.Stat(canonicalPrompt)
	if err != nil {
		return task{}, fmt.Errorf("inspect prompt %q: %w", canonicalPrompt, err)
	}
	if !info.Mode().IsRegular() {
		return task{}, fmt.Errorf("prompt %q is not a regular file", canonicalPrompt)
	}
	prompt, err := os.ReadFile(canonicalPrompt)
	if err != nil {
		return task{}, fmt.Errorf("read prompt %q: %w", canonicalPrompt, err)
	}
	if len(strings.TrimSpace(string(prompt))) == 0 {
		return task{}, fmt.Errorf("prompt %q is empty or whitespace-only", canonicalPrompt)
	}

	memoryDir := cfg.memoryDir
	if memoryDir == "" {
		memoryDir = filepath.Join(filepath.Dir(canonicalPrompt), "memories")
	} else if !filepath.IsAbs(memoryDir) {
		memoryDir = filepath.Join(workDir, memoryDir)
	}
	memoryDir, err = filepath.Abs(memoryDir)
	if err != nil {
		return task{}, fmt.Errorf("resolve memory directory: %w", err)
	}

	return task{
		promptPath: canonicalPrompt,
		prompt:     prompt,
		taskID:     pathID(canonicalPrompt),
		memoryDir:  filepath.Clean(memoryDir),
		workDir:    workDir,
	}, nil
}

// pathID returns a deterministic identifier for a canonical prompt path.
func pathID(path string) string {
	digest := sha256.Sum256([]byte(path))
	return hex.EncodeToString(digest[:])
}

// localStateDir returns the fixed per-user directory used for advisory lock files.
func localStateDir() (string, error) {
	if runtime.GOOS == "linux" {
		if stateHome := os.Getenv("XDG_STATE_HOME"); stateHome != "" {
			if !filepath.IsAbs(stateHome) {
				return "", errors.New("XDG_STATE_HOME must be an absolute path")
			}
			return filepath.Join(stateHome, "codex-task"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(home, ".local", "state", "codex-task"), nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user state directory: %w", err)
	}
	return filepath.Join(configDir, "codex-task"), nil
}
