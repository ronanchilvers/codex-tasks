# Implementation Plan

## Objective

Implement [Specification.md](Specification.md) as a small Go binary that runs one Markdown prompt through `codex exec`, prevents overlapping executions of that task, and stores output for later reference.

The project currently contains the specification only. This document proposes the implementation sequence; it does not implement the tool.

## Scope and assumptions

- Use `codex-task` as the provisional binary name.
- Cron owns scheduling. Each invocation runs one task and exits.
- Interpret “only executed once” as one active execution per prompt file. Later scheduled runs remain allowed.
- Identify a task by its canonical absolute prompt path, resolving symbolic links. Editing the file does not change its identity; separate files with identical contents remain separate tasks. Hard-link aliases are outside this initial identity guarantee.
- Target Linux and macOS initially, reflecting cron usage. Build a separate binary for each operating system and architecture. Windows support remains a scope decision before platform-specific implementation.
- Codex must already be installed, authenticated, and available through the execution environment's `PATH`.
- Confirmed memory workflow: store local Markdown files that later prompts reference explicitly by path.
- Keep the first version free of an internal scheduler, configuration file format, prompt templates, automatic retries, and a database.

## Proposed command interface

```sh
codex-task --prompt ./prompts/daily-review.md
codex-task --prompt ./prompts/daily-review.md --memory-dir /srv/memories
codex-task --prompt /srv/tasks/daily-review.md --dry-run
codex-task --prompt ./prompts/daily-review.md -- --model MODEL --sandbox workspace-write
```

| Input | Proposed behavior |
| --- | --- |
| `--prompt PATH` | Required path to a readable, nonempty Markdown prompt file. Resolve relative paths against the invocation's current working directory. |
| `--memory-dir PATH` | Optional directory for memory Markdown files. Resolve relative paths against the invocation's current working directory. Default to `memories/` beside the canonical prompt file. |
| `--dry-run` | Show the resolved prompt path, exact prompt text, execution directory, resolved memory directory, and quoted command without launching Codex or writing state. |
| `--help` | Explain wrapper flags, forwarding, locking, output, and exit behavior. |
| Arguments after `--` | Forward as separate arguments to `codex exec`, preserving order and values. |

Use Go's standard flag parser for wrapper flags. Do not execute a shell or reinterpret forwarded arguments as shell syntax. The prompt comes exclusively from the file, so additional positional prompts and alternate subcommands are outside the supported interface.

The wrapper owns standard input and `--output-last-message`. Detect conflicting forwarded output flags, including short and equals forms, and return a clear usage error. Avoid recreating Codex's entire option parser; validate wrapper conflicts and let Codex validate its other options.

Inherit the invocation's working directory and environment. A forwarded Codex `--cd` affects Codex's workspace, not how the wrapper resolves prompt or memory paths.

## Execution design

1. Parse wrapper arguments and separate forwarded options.
2. Resolve and validate the prompt file, then read its contents once.
3. Derive the task identifier from the canonical prompt path and resolve the memory directory.
4. Construct the command and return the preview immediately for a dry-run.
5. Resolve the Codex executable and prepare the writable local state and memory directories. Report directory creation or permission failures before launching Codex.
6. Acquire a nonblocking lock for the task. If already locked, report that the run was skipped and exit without starting Codex.
7. Create a private temporary output file and invoke `codex exec` with the forwarded options, the wrapper-owned output path, and `-` to read the prompt from standard input.
8. Stream child standard output and standard error to the corresponding parent streams. Capture the final response through the dedicated output file.
9. Wait for the child, store the result as a local Markdown file, and report its absolute path on standard error.
10. Clean up temporary resources and release the lock on success and failure.

The installed `codex exec --help` confirms the `-` standard-input convention. Official OpenAI documentation describes `--output-last-message` for capturing the final response independently of streamed output. This also avoids treating `--json` event output as the memory body. See [Non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode).

### Locking

- Use a deterministic hash of the canonical path as the lock filename.
- Keep locks in a fixed per-user local state directory, independent of the invocation directory and memory destination. The guarantee covers cooperating invocations by the same operating-system user on one host.
- Use an operating-system advisory lock held for the complete execution and persistence sequence. Prefer one small dependency if it avoids fragile platform-specific code.
- Do not use mere file existence as the lock: crashes would leave stale state. Do not unlink an advisory lock file during normal cleanup, as this can let concurrent processes lock different files at the same path.
- Allow different task identifiers to run concurrently. Skip a duplicate immediately rather than queueing it.
- Forward termination signals and wait for the child to stop before releasing the lock. Test descendant-process cleanup on supported systems.
- Explicitly document the forced-kill boundary: killing the wrapper without cleanup can release its lock while a surviving child continues. Before claiming crash-safe exclusion, verify a lock ownership or supervision approach that covers that case. Otherwise document the limitation and require whole-process-group termination operationally.

### Output and failure behavior

- Treat the final agent response as the candidate memory, with task path, start/end timestamps, and completion status as metadata.
- Do not mark a failed or interrupted run as a successful memory. Retain useful partial output as a failed-run artifact when available.
- A successful child with missing or empty final output is a capture failure and returns nonzero.
- If persistence fails after Codex succeeds, return nonzero and preserve recoverable output where possible, reporting its path.
- Proposed wrapper exit codes: `0` for successful execution and persistence, `1` for wrapper execution or persistence failure, `2` for usage errors, and `75` for an already-running task. Propagate nonzero child exit codes, and distinguish their origin in diagnostics. Document that numeric codes can overlap.
- Do not automatically retry: a task may already have made changes before failing.

## Implementation phases

### 1. Establish the minimal Go command

Create `go.mod`, a thin `main.go`, and small files in the same package for arguments, execution, locking, and memory as those behaviors are implemented. Use the standard library wherever practical. Add conventional Go documentation comments to new functions and types.

**Verification:** The command builds; help succeeds; missing or unknown wrapper flags fail with clear usage output.

### 2. Implement prompt loading and dry-run

Implement path resolution, file validation, prompt reading, forwarding separation, and a shared command-construction path for previews and real runs. Reject directories, unreadable files, and whitespace-only prompts. Preserve the original prompt bytes when sending valid content.

A dry-run must work without Codex installed or authenticated. Show a clearly marked placeholder for the temporary output path without creating it. The displayed command is a readable rendering of the argument vector, never the execution mechanism.

**Verification:** Test relative and absolute paths, symbolic links, spaces, Unicode, empty and missing files, and options containing quotes or shell metacharacters. Assert that a dry-run launches no subprocess and creates no state files.

### 3. Implement subprocess execution and capture

Use `os/exec` to launch Codex directly, send the prompt through standard input, stream diagnostics, and collect the final output file. Handle executable lookup failures, child exit status, and termination. Keep process orchestration callable without exiting inside helper functions so cleanup always runs.

**Verification:** Use a fake executable to assert exact arguments, prompt bytes, inherited working directory, streamed output, final-response capture, nonzero exits, and missing output. Tests must not depend on credentials, network access, or live model calls.

### 4. Add task exclusion

Implement canonical task identifiers and advisory locking in the fixed state directory. Hold the lock until child termination and output handling finish. Keep the lock implementation small and explicitly scoped to supported operating systems.

**Verification:** Run competing subprocesses and confirm exactly one executes the same prompt, including relative-path and symbolic-link aliases. Confirm different prompts can run concurrently and later runs succeed after normal completion, launch failure, child failure, and handled termination. Exercise the forced-kill case and record the supported guarantee.

### 5. Implement local Markdown memories

Store one Markdown record per run. Later prompts explicitly name the record to read.

- Use `--memory-dir PATH` when supplied; otherwise default to `memories/` beside the canonical prompt file. Resolve an explicit relative path against the invocation directory.
- Store records in `<memory-dir>/<task-id>/` to keep tasks separate when they share a memory directory. Create missing directories for real runs only. Changing the memory directory does not change task identity or locking.
- Use a timestamp and unique suffix for each filename so repeated runs preserve earlier records.
- Include the canonical prompt path, start/end timestamps, completion status, and final agent response in each record. Store timestamps in Coordinated Universal Time (UTC).
- Write a temporary file in the destination directory, close it successfully, and atomically rename it to its final name. Use owner-only directory and file permissions where supported.
- Clearly distinguish failed-run artifacts from successful memories using a `failed-` filename prefix and status metadata.
- Report the saved record's absolute path on standard error so it can be copied into a later prompt.
- Document that the referenced file must be readable within the later Codex session's allowed workspace.

For example, a later prompt could contain:

```text
Read /srv/tasks/memories/<task-id>/<saved-run>.md for the previous review.
Check whether the issues identified in that review have been resolved.
```

The wrapper passes this prompt through unchanged. Codex reads the explicitly referenced file during the session.

**Verification:** Test default, explicit absolute, and explicit relative memory directories, including spaces, missing directories, and unwritable destinations. Confirm dry-run reports the destination without creating it and that changing destinations cannot bypass the same-task lock. A later task can read a saved record; repeated runs preserve earlier records; failed writes never appear as complete records; failed runs are clearly identified; and reported paths match the files written. Use a fake executable for persistence checks and the release smoke test for actual reference retrieval.

### 6. Document and verify cron operation

Add a concise README covering installation, required Codex setup, flags, memory retrieval, lock scope, exit codes, and supported platforms. Include an example with an explicit working directory and executable paths:

```cron
PATH=/usr/local/bin:/usr/bin:/bin
0 7 * * * cd /srv/project && /usr/local/bin/codex-task --prompt /srv/tasks/daily-review.md --memory-dir /srv/memories >> /srv/log/codex-task.log 2>&1
```

Explain that paths must match the host, log directories must exist, and Codex authentication and configuration must be available to the cron user. Document explicit forwarding of options needed by the task's environment.

**Verification:** Run `gofmt`, `go vet ./...`, `go test ./...`, and `go build ./...`. Cross-build the chosen Linux and macOS architecture targets and exercise subprocess and locking tests natively on both operating systems. Perform a fake-Codex run with a minimal cron-like environment. Before release, perform one deliberate live smoke test that verifies final-output capture and explicit retrieval of a saved Markdown memory.

## Acceptance checklist

- [ ] A single Go binary runs a Markdown file as the Codex prompt.
- [ ] Relative and absolute prompt paths behave consistently.
- [ ] `--memory-dir` selects the storage location, with documented defaults and relative-path behavior.
- [ ] Forwarded Codex options preserve argument boundaries.
- [ ] Dry-run accurately previews the prompt and command without execution or state changes.
- [ ] The same task cannot overlap within the documented lock scope; distinct tasks can run concurrently.
- [ ] Output is durably stored in local Markdown files and demonstrably usable by a later prompt that explicitly references a saved path.
- [ ] Failures and interruptions produce useful diagnostics and documented exit behavior.
- [ ] Builds and focused tests pass on the supported platforms.
- [ ] Cron setup and operational limitations are documented.
