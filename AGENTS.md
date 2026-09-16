# Repository Guidelines

## Project Structure & Module Organization

This repository contains the `codex-task` Go command-line application. Production Go files live at the repository root and use `package main`; `main.go` is the entry point, while files such as `arguments.go`, `task.go`, `execution.go`, and `memory.go` organize the application by responsibility. Keep platform-specific code in files such as `lock_unix.go` and `process_unix.go`.

Unit tests sit beside their implementation in `*_test.go` files. Keep user-facing examples in `examples/` and design or implementation notes in `docs/`. Do not commit built binaries such as `codex-task`.

## Build, Test, and Development Commands

- `go build -o codex-task .` builds the command for the current platform.
- `go test ./...` runs the complete unit-test suite.
- `go test -run TestLoadTask ./...` runs a focused test while iterating.
- `go vet ./...` performs Go's static correctness checks.
- `gofmt -w *.go` formats root-level Go source files before committing.

Run the built command with `./codex-task --help`. Use `--dry-run` and a temporary prompt when checking behavior that would otherwise start Codex or write memory records.

## Coding Style & Naming Conventions

Target Go 1.23 and follow standard Go formatting: tabs for indentation and `gofmt` for all Go changes. Use descriptive lowerCamelCase names for unexported identifiers and PascalCase only for exported identifiers. Keep functions small and place each concern in its existing file rather than introducing packages prematurely. Add Go doc comments to new top-level functions and types when their purpose is not obvious from context.

Prefer direct errors with actionable context. Preserve the command's existing exit-code, path-resolution, locking, and atomic-write behavior when modifying execution flows.

## Testing Guidelines

Write table-driven tests where multiple inputs share expected behavior. Name tests `Test<Behavior>` and subtests for cases, for example `TestLoadTaskRejectsInvalidFiles`. Use `t.TempDir()` for filesystem state and avoid invoking a real `codex` process in unit tests. Run `go test ./...` and `go vet ./...` before opening a pull request.

## Commit & Pull Request Guidelines

Use short, imperative commit subjects, matching history: `Add task smoke test` or `Support frontmatter keys for task_id, model and effort`. Keep each commit scoped to one change. Pull requests should explain the behavior change, note tests run, link the relevant issue when available, and include terminal output or screenshots only when they clarify a user-visible command-line change.
