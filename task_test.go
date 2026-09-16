package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadTaskPaths verifies canonical prompt identity and memory path resolution.
func TestLoadTaskPaths(t *testing.T) {
	temporary := t.TempDir()
	promptDir := filepath.Join(temporary, "prompts with spaces")
	if err := os.Mkdir(promptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(promptDir, "日本語.md")
	prompt := []byte("Review café changes.\n")
	if err := os.WriteFile(promptPath, prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(temporary, "alias.md")
	if err := os.Symlink(promptPath, alias); err != nil {
		t.Fatal(err)
	}

	direct, err := loadTask(config{promptPath: promptPath})
	if err != nil {
		t.Fatal(err)
	}
	linked, err := loadTask(config{promptPath: alias, memoryDir: "relative memories"})
	if err != nil {
		t.Fatal(err)
	}
	canonicalPrompt, err := filepath.EvalSymlinks(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if direct.promptPath != canonicalPrompt || linked.promptPath != canonicalPrompt || direct.taskID != linked.taskID {
		t.Fatalf("canonical identities differ: %#v, %#v", direct, linked)
	}
	if !bytes.Equal(direct.prompt, prompt) {
		t.Fatalf("prompt = %q, want %q", direct.prompt, prompt)
	}
	if direct.memoryDir != filepath.Join(filepath.Dir(canonicalPrompt), "memories") {
		t.Fatalf("default memory directory = %q", direct.memoryDir)
	}
	if !filepath.IsAbs(linked.memoryDir) || filepath.Base(linked.memoryDir) != "relative memories" {
		t.Fatalf("relative memory directory = %q", linked.memoryDir)
	}
}

// TestLoadTaskRejectsInvalidFiles verifies missing, directory, and blank prompt failures.
func TestLoadTaskRejectsInvalidFiles(t *testing.T) {
	temporary := t.TempDir()
	blank := filepath.Join(temporary, "blank.md")
	if err := os.WriteFile(blank, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(temporary, "missing.md"), temporary, blank} {
		if _, err := loadTask(config{promptPath: path}); err == nil {
			t.Errorf("loadTask(%q) unexpectedly succeeded", path)
		}
	}
}

// TestDryRunHasNoStateChanges verifies preview output without execution or directory creation.
func TestDryRunHasNoStateChanges(t *testing.T) {
	temporary := t.TempDir()
	promptPath := filepath.Join(temporary, "prompt.md")
	prompt := "exact prompt without newline"
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}
	memoryDir := filepath.Join(temporary, "not-created")
	selected, err := loadTask(config{promptPath: promptPath, memoryDir: memoryDir})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	printDryRun(&output, selected, []string{"--config", `x='a b;$(safe)'`})
	preview := output.String()
	for _, expected := range []string{promptPath, memoryDir, prompt, outputPlaceholder, `'x='"'"'a b;$(safe)'"'"''`} {
		if !strings.Contains(preview, expected) {
			t.Errorf("preview does not contain %q:\n%s", expected, preview)
		}
	}
	if _, err := os.Stat(memoryDir); !os.IsNotExist(err) {
		t.Fatalf("dry-run destination exists or returned unexpected error: %v", err)
	}
}
