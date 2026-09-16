# Specification

**Description:** A portable binary, written in Go which can be used to run customised `codex exec` sessions from cron

## Goals
- Lightweight codebase written with Go as simply as possible
- Ability to run a `codex exec` session with the contents of a given markdown file as the prompt
- Ability to trap the output of the `exec` session and store it as a memory which can later be referenced
- Locking to ensure that a specific task prompt can only be executed once - concurrent runs of the same prompt are not allowed

## UX
- Command line flags:
  - Specify the path (relative to CWD or absolute) to the markdown prompt to use
  - Support passing additional options to the underlying `codex exec` process
  - Support for a dry-run which shows what prompt would be used and the `codex CLI` command that would be executed
