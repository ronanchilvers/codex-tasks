package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// task is a prompt resolved to its stable identity and output destination.
type task struct {
	promptPath string
	prompt     []byte
	taskID     string
	model      string
	effort     string
	memoryDir  string
	workDir    string
}

// taskFrontMatter contains task metadata extracted from a prompt's leading front-matter block.
type taskFrontMatter struct {
	taskID string
	model  string
	effort string
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
	metadata, prompt, err := parseTaskFrontMatter(prompt)
	if err != nil {
		return task{}, fmt.Errorf("parse prompt front matter: %w", err)
	}
	if len(strings.TrimSpace(string(prompt))) == 0 {
		return task{}, fmt.Errorf("prompt %q is empty or whitespace-only", canonicalPrompt)
	}
	if metadata.taskID == "" {
		return task{}, fmt.Errorf("prompt %q is missing front matter task_id; add:\n---\ntask_id: example-task\n---", canonicalPrompt)
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
		taskID:     metadata.taskID,
		model:      metadata.model,
		effort:     metadata.effort,
		memoryDir:  filepath.Clean(memoryDir),
		workDir:    workDir,
	}, nil
}

// parseTaskFrontMatter extracts supported fields from a leading YAML-style front-matter block.
// It returns the prompt body without that block so task metadata is not sent to Codex.
func parseTaskFrontMatter(prompt []byte) (taskFrontMatter, []byte, error) {
	const delimiter = "---"
	text := string(prompt)
	firstLine, remainder, found := strings.Cut(text, "\n")
	if !found || strings.TrimSuffix(firstLine, "\r") != delimiter {
		return taskFrontMatter{}, prompt, nil
	}

	var metadata taskFrontMatter
	seen := make(map[string]bool)
	for {
		line, next, hasNext := strings.Cut(remainder, "\n")
		trimmedLine := strings.TrimSpace(line)
		if trimmedLine == delimiter || trimmedLine == "..." {
			return metadata, []byte(next), nil
		}
		if !hasNext {
			return taskFrontMatter{}, nil, errors.New("front matter is missing a closing --- delimiter")
		}
		key, value, isField := strings.Cut(line, ":")
		if !isField {
			remainder = next
			continue
		}
		field := strings.TrimSpace(key)
		if field != "task_id" && field != "model" && field != "effort" {
			remainder = next
			continue
		}
		if seen[field] {
			return taskFrontMatter{}, nil, fmt.Errorf("front matter contains more than one %s", field)
		}
		seen[field] = true

		parsed, err := parseFrontMatterValue(field, value)
		if err != nil {
			return taskFrontMatter{}, nil, err
		}
		switch field {
		case "task_id":
			if !validTaskID(parsed) {
				return taskFrontMatter{}, nil, errors.New("task_id must contain 1 to 128 letters, digits, hyphens, or underscores")
			}
			metadata.taskID = parsed
		case "model":
			metadata.model = parsed
		case "effort":
			metadata.effort = parsed
		}
		remainder = next
	}
}

// parseFrontMatterValue parses a nonempty simple YAML scalar for a supported task field.
func parseFrontMatterValue(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "\"") {
		parsed, err := strconv.Unquote(value)
		if err != nil {
			return "", fmt.Errorf("invalid quoted %s: %w", field, err)
		}
		value = parsed
	} else if strings.HasPrefix(value, "'") {
		if len(value) < 2 || !strings.HasSuffix(value, "'") {
			return "", fmt.Errorf("invalid quoted %s", field)
		}
		value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	} else if comment := strings.Index(value, " #"); comment >= 0 {
		value = strings.TrimSpace(value[:comment])
	}
	if value == "" {
		return "", fmt.Errorf("%s must not be empty", field)
	}
	return value, nil
}

// validTaskID reports whether an identifier is safe for memory directory and lock filenames.
func validTaskID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
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
