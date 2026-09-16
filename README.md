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
codex-task --prompt PATH [--memory-dir PATH] [--memory-count N] [--dry-run] [-- CODEX_OPTIONS...]
```

- `--prompt PATH` is required. Relative paths use the invocation's current
  working directory. The file must be regular, readable, nonempty, and declare
  a `task_id` in leading YAML front matter.
- `--memory-dir PATH` chooses the record root. Relative paths use the invocation
  directory. The default is `memories/` beside the canonical prompt.
- `--memory-count N` prepends the most recent `N` successful records for the
  task to the prompt, in chronological order. It defaults to `3`; use `0` to
  disable memory injection.
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

Before each run, the wrapper automatically prepends the selected successful
records as prior-task context. Failed-run records are not injected. The records
must be readable by the wrapper process.

Set a stable task identifier in leading YAML front matter to preserve memory
and locking identity when a prompt is renamed or moved. `model` and `effort`
are optional Codex settings:

```markdown
---
model: gpt-5.6-luna
effort: high
task_id: daily-code-review
---

Review the repository for changes that need attention.
```

Task IDs are required and may contain 1 to 128 letters, digits, hyphens, or
underscores. The front-matter block is not sent to Codex. `model` is passed as
`--model`; `effort` is passed as Codex's `model_reasoning_effort` setting. If
the same setting is also given after `--`, the wrapper fails before Codex
starts and asks you to remove one. A prompt without a `task_id` fails before
Codex starts and prints the required snippet.

## Locking and failures

The front-matter `task_id` identifies a task. Cooperating invocations by one
operating-system user on one host use a nonblocking advisory lock in the user
state directory. On Linux this is `$XDG_STATE_HOME/codex-task`, where XDG
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
