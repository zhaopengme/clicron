package mcp

import (
	"fmt"
)

// BuildClaudeCommand builds a complete claude CLI command from a prompt.
// The command is constructed to run in non-interactive mode with JSON output.
func BuildClaudeCommand(prompt string) string {
	// Build the claude command with:
	// -p: execute prompt and exit (non-interactive)
	// --output-format json: structured output for parsing
	// --dangerously-skip-permissions: skip permission checks for automation
	return fmt.Sprintf("claude -p %q --output-format json --dangerously-skip-permissions", prompt)
}

// BuildCommand builds a command from a prompt using the specified engine.
// Currently only "claude" is supported, but this is designed to be extensible.
func BuildCommand(prompt string, engine string) string {
	switch engine {
	case "claude", "":
		return BuildClaudeCommand(prompt)
	default:
		// For unknown engines, default to claude
		return BuildClaudeCommand(prompt)
	}
}
