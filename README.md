# codex-task

`codex-task` is a small Go wrapper for running one Markdown prompt through
`codex exec` from cron. It streams Codex output, prevents overlapping runs of
the same prompt, and stores the final agent response in a local Markdown record.

## Install

Build for the current machine:

```sh
go build -o codex-task .
```

Codex must already be installed, authenticated, configured, and available in
the invoking user's `PATH`. Linux and macOS are supported. Build a separate
binary for each operating system and architecture.

## Usage

```text
codex-task --prompt PATH [--memory-dir PATH] [--dry-run] [-- CODEX_OPTIONS...]
```

- `--prompt PATH` is required. Relative paths use the invocation's current
  working directory. Symbolic links are resolved before task identity is
  calculated. The file must be regular, readable, and nonempty.
- `--memory-dir PATH` chooses the record root. Relative paths use the invocation
  directory. The default is `memories/` beside the canonical prompt.
- `--dry-run` prints resolved paths, exact prompt bytes, and a quoted command
  preview. It does not find or launch Codex and creates no directories or files.
- Arguments after `--` are passed directly to `codex exec`. For example:

  ```sh
  codex-task --prompt ./daily.md -- --model MODEL --sandbox workspace-write
  ```

The wrapper owns standard input and `-o`/`--output-last-message`, so those
options cannot be forwarded. It invokes Codex directly without a shell and
inherits the working directory and environment. A forwarded `--cd` changes the
Codex workspace only; it does not affect wrapper path resolution.

## Memories

Each record is written atomically to `<memory-dir>/<task-id>/`, with the prompt
path, Coordinated Universal Time (UTC) timestamps, completion status, and final
agent response. Successful files use a timestamp and unique suffix. Records
from failed Codex runs have a `failed-` prefix when a final response is
available. The absolute saved path is reported on standard error.

A later prompt can refer to a record explicitly:

```text
Read /srv/memories/<task-id>/<saved-run>.md for the previous review.
Check whether the issues identified in that review have been resolved.
```

The record must be readable within that later Codex session's allowed
workspace. `codex-task` does not inject memories automatically.

## Locking and failures

The canonical absolute prompt path identifies a task. Cooperating invocations
by one operating-system user on one host use a nonblocking advisory lock in the
user state directory. On Linux this is `$XDG_STATE_HOME/codex-task`, where XDG
means Cross-Desktop Group, or `$HOME/.local/state/codex-task` when that variable
is unset. On macOS it is `$HOME/Library/Application Support/codex-task`.
Different prompts may run concurrently. A duplicate is skipped rather than
queued. Lock files remain on disk intentionally; the operating system releases
their locks after normal exits and crashes.

Signals for interrupt, termination, and hangup are forwarded to the entire
Codex process group, and the wrapper waits before releasing its lock. A forced
kill of the wrapper cannot run cleanup: the operating system releases its lock
while an independently surviving child might continue. Operational supervisors
must terminate the whole process group when using an uncatchable forced kill.

Exit codes are `0` for execution and persistence success, `1` for wrapper or
persistence failure, `2` for usage errors, and `75` when the task is already
running. A nonzero Codex exit code is propagated, so numeric meanings can
overlap. Runs are never retried automatically.

## Cron

Use explicit directories and executable paths:

```cron
PATH=/usr/local/bin:/usr/bin:/bin
0 7 * * * cd /srv/project && /usr/local/bin/codex-task --prompt /srv/tasks/daily-review.md --memory-dir /srv/memories >> /srv/log/codex-task.log 2>&1
```

Adjust every path for the host and create the log directory first. Codex
authentication and configuration must be available to the cron user. Forward
any Codex options required by the task environment explicitly after `--`.
