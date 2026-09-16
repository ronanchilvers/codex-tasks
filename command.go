package main

import (
	"fmt"
	"io"
	"strings"
)

const outputPlaceholder = "<temporary-output-file>"

// commandArguments builds the exact argument vector used for preview or execution.
func commandArguments(forwarded []string, outputPath string) []string {
	args := []string{"exec"}
	args = append(args, forwarded...)
	args = append(args, "--output-last-message", outputPath, "-")
	return args
}

// printDryRun writes a side-effect-free description of the resolved task.
func printDryRun(output io.Writer, current task, forwarded []string) {
	fmt.Fprintf(output, "Prompt path: %s\n", current.promptPath)
	fmt.Fprintf(output, "Execution directory: %s\n", current.workDir)
	fmt.Fprintf(output, "Memory directory: %s\n", current.memoryDir)
	fmt.Fprintf(output, "Task ID: %s\n", current.taskID)
	fmt.Fprintf(output, "Prompt (%d bytes, exact contents between markers):\n", len(current.prompt))
	fmt.Fprintln(output, "----- BEGIN PROMPT -----")
	_, _ = output.Write(current.prompt)
	if current.prompt[len(current.prompt)-1] != '\n' {
		fmt.Fprintln(output)
	}
	fmt.Fprintln(output, "----- END PROMPT -----")
	fmt.Fprintln(output, "Command (display only; no shell is used):")
	parts := []string{quoteArgument("codex")}
	for _, arg := range commandArguments(forwarded, outputPlaceholder) {
		parts = append(parts, quoteArgument(arg))
	}
	fmt.Fprintln(output, strings.Join(parts, " "))
}

// quoteArgument renders one argument legibly using POSIX shell-style single quoting.
func quoteArgument(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
}
