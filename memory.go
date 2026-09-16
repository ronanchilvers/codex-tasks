package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// randomSuffix generates a short collision-resistant filename suffix.
func randomSuffix() (string, error) {
	bytes := make([]byte, 6)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate memory filename: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}
