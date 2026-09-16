---
task_id: task-runner-smoke-test
model: gpt-5.6-luna
effort: low
---
# Task runner smoke test

Look for saved task records under `examples/memories/`. If records exist, read
the most recently modified one to confirm the task runner's memory is available.

Create or overwrite `examples/task-runner-output.txt` with exactly these two
lines:

```text
Task runner smoke test
Previous memory: found
```

Use `Previous memory: none` instead when no saved record exists. End your final
response by stating which value you wrote.
