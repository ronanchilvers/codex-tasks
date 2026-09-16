package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadTaskPaths verifies canonical prompt resolution and memory path resolution.
func TestLoadTaskPaths(t *testing.T) {
	temporary := t.TempDir()
	promptDir := filepath.Join(temporary, "prompts with spaces")
	if err := os.Mkdir(promptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(promptDir, "日本語.md")
	prompt := []byte("---\nmodel: gpt-5.6-luna\neffort: high\ntask_id: path-test\n---\nReview café changes.\n")
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
	if !bytes.Equal(direct.prompt, []byte("Review café changes.\n")) {
		t.Fatalf("prompt = %q", direct.prompt)
	}
	if direct.model != "gpt-5.6-luna" || direct.effort != "high" {
		t.Fatalf("model and effort = %q and %q", direct.model, direct.effort)
	}
	if direct.memoryDir != filepath.Join(filepath.Dir(canonicalPrompt), "memories") {
		t.Fatalf("default memory directory = %q", direct.memoryDir)
	}
	if !filepath.IsAbs(linked.memoryDir) || filepath.Base(linked.memoryDir) != "relative memories" {
		t.Fatalf("relative memory directory = %q", linked.memoryDir)
	}
}

// TestLoadTaskRequiresTaskID verifies prompts without a declared identity get actionable guidance.
func TestLoadTaskRequiresTaskID(t *testing.T) {
	temporary := t.TempDir()
	path := filepath.Join(temporary, "prompt.md")
	if err := os.WriteFile(path, []byte("Review the changes.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadTask(config{promptPath: path})
	if err == nil || !strings.Contains(err.Error(), "missing front matter task_id") || !strings.Contains(err.Error(), "task_id: example-task") {
		t.Fatalf("loadTask error = %v", err)
	}
}

// TestLoadTaskUsesFrontMatterTaskID verifies that a declared identity survives a path change.
func TestLoadTaskUsesFrontMatterTaskID(t *testing.T) {
	temporary := t.TempDir()
	prompt := []byte("---\ntask_id: daily-code-review\n---\nReview the changes.\n")
	firstPath := filepath.Join(temporary, "first.md")
	secondPath := filepath.Join(temporary, "moved", "second.md")
	if err := os.WriteFile(firstPath, prompt, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(secondPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondPath, prompt, 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := loadTask(config{promptPath: firstPath})
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadTask(config{promptPath: secondPath})
	if err != nil {
		t.Fatal(err)
	}
	if first.taskID != "daily-code-review" || second.taskID != first.taskID {
		t.Fatalf("task IDs = %q and %q", first.taskID, second.taskID)
	}
	if string(first.prompt) != "Review the changes.\n" {
		t.Fatalf("prompt = %q", first.prompt)
	}
}

// TestLoadTaskRejectsInvalidTaskID verifies task IDs cannot escape their storage directory.
func TestLoadTaskRejectsInvalidTaskID(t *testing.T) {
	temporary := t.TempDir()
	for name, prompt := range map[string]string{
		"empty":            "---\ntask_id:\n---\nPrompt\n",
		"unsafe":           "---\ntask_id: ../other\n---\nPrompt\n",
		"duplicate":        "---\ntask_id: first\ntask_id: second\n---\nPrompt\n",
		"empty-model":      "---\ntask_id: valid\nmodel:\n---\nPrompt\n",
		"duplicate-effort": "---\ntask_id: valid\neffort: low\neffort: high\n---\nPrompt\n",
		"unterminated":     "---\ntask_id: valid\nPrompt\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(temporary, name+".md")
			if err := os.WriteFile(path, []byte(prompt), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadTask(config{promptPath: path}); err == nil {
				t.Fatal("loadTask unexpectedly succeeded")
			}
		})
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
	if err := os.WriteFile(promptPath, []byte("---\ntask_id: dry-run-test\n---\n"+prompt), 0o600); err != nil {
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
