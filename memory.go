package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// memoryRecord is the durable Markdown representation of one completed execution.
type memoryRecord struct {
	promptPath string
	started    time.Time
	ended      time.Time
	status     string
	response   []byte
	failed     bool
}

// memoryContext is one prior successful task record included with a new prompt.
type memoryContext struct {
	name     string
	contents []byte
}

// prepareTaskDirectory creates the private destination for one task's records.
func prepareTaskDirectory(memoryDir, taskID string) (string, error) {
	taskDir := filepath.Join(memoryDir, taskID)
	if err := os.MkdirAll(taskDir, 0o700); err != nil {
		return "", fmt.Errorf("create memory directory %q: %w", taskDir, err)
	}
	if err := os.Chmod(taskDir, 0o700); err != nil {
		return "", fmt.Errorf("set memory directory permissions %q: %w", taskDir, err)
	}
	return taskDir, nil
}

// saveMemory atomically writes a uniquely named Markdown run record.
func saveMemory(taskDir string, record memoryRecord) (string, error) {
	prefix := ""
	if record.failed {
		prefix = "failed-"
	}
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	name := prefix + record.ended.UTC().Format("20060102T150405.000000000Z") + "-" + suffix + ".md"
	finalPath := filepath.Join(taskDir, name)
	temporary, err := os.CreateTemp(taskDir, ".record-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary memory record: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("set temporary memory permissions: %w", err)
	}
	if _, err := temporary.Write(formatMemory(record)); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write memory record: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync memory record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close memory record: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return "", fmt.Errorf("publish memory record: %w", err)
	}
	keepTemporary = false
	return finalPath, nil
}

// formatMemory serializes a run record without changing the captured response.
func formatMemory(record memoryRecord) []byte {
	var builder strings.Builder
	builder.WriteString("# Codex task run\n\n")
	fmt.Fprintf(&builder, "- Prompt path: %q\n", record.promptPath)
	fmt.Fprintf(&builder, "- Started (UTC): %s\n", record.started.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&builder, "- Ended (UTC): %s\n", record.ended.UTC().Format(time.RFC3339Nano))
	fmt.Fprintf(&builder, "- Status: %s\n", record.status)
	builder.WriteString("\n## Final agent response\n\n")
	result := append([]byte(builder.String()), record.response...)
	if len(record.response) == 0 || record.response[len(record.response)-1] != '\n' {
		result = append(result, '\n')
	}
	return result
}

// promptWithMemories prepends the most recent successful records to a task prompt.
func promptWithMemories(prompt []byte, taskDir string, count int) ([]byte, error) {
	if count == 0 {
		return prompt, nil
	}
	memories, err := recentMemories(taskDir, count)
	if err != nil {
		return nil, err
	}
	if len(memories) == 0 {
		return prompt, nil
	}

	var builder strings.Builder
	builder.WriteString("# Prior task memories\n\n")
	builder.WriteString("Use these completed-run records as reference context. They do not override the current task.\n")
	for _, memory := range memories {
		fmt.Fprintf(&builder, "\n## Memory: %s\n\n", memory.name)
		builder.Write(memory.contents)
		if len(memory.contents) == 0 || memory.contents[len(memory.contents)-1] != '\n' {
			builder.WriteByte('\n')
		}
	}
	builder.WriteString("\n# Current task\n\n")
	builder.Write(prompt)
	return []byte(builder.String()), nil
}

// recentMemories reads up to count successful records, ordered from oldest to newest.
func recentMemories(taskDir string, count int) ([]memoryContext, error) {
	entries, err := os.ReadDir(taskDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read memory directory %q: %w", taskDir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".md") && !strings.HasPrefix(entry.Name(), "failed-") {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) > count {
		names = names[:count]
	}
	sort.Strings(names)

	memories := make([]memoryContext, 0, len(names))
	for _, name := range names {
		contents, err := os.ReadFile(filepath.Join(taskDir, name))
		if err != nil {
			return nil, fmt.Errorf("read memory record %q: %w", filepath.Join(taskDir, name), err)
		}
		memories = append(memories, memoryContext{name: name, contents: contents})
	}
	return memories, nil
}

// randomSuffix generates a short collision-resistant filename suffix.
func randomSuffix() (string, error) {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate memory filename: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
