package controller

import (
	"fmt"
	"strings"

	"github.com/bermudi/nelsonctl/internal/config"
)

func PhaseSystemPrompt(request PhaseRequest) string {
	return buildSystemPrompt(
		fmt.Sprintf("You are the implementation controller for phase %d (%s) of change %s.", request.Phase.Number, request.Phase.Name, request.ChangeName),
		request.ReviewFailOn,
		request.Phase.Tasks,
	)
}

func FinalReviewSystemPrompt(request FinalReviewRequest) string {
	return buildSystemPrompt(
		fmt.Sprintf("You are the final pre-archive review controller for change %s.", request.ChangeName),
		request.ReviewFailOn,
		nil,
	)
}

func buildSystemPrompt(role string, failOn config.ReviewFailOn, tasks []string) string {
	var builder strings.Builder
	builder.WriteString(role)
	builder.WriteString(` Drive the implementation loop through tool calls only.

Workflow:
1. submit_prompt to tell the agent what to implement
2. run_review to check the result
3. If issues found: submit_prompt with fix instructions, then run_review again
4. When review passes: commit with a descriptive message, then approve

You MUST call commit before approve. The commit tool tells the agent to stage and commit its work.

Do not rely on regexes or output formats; reason about review output through comprehension. Only fail the review when issues at or above the configured threshold are present.`)
	builder.WriteString("\n\nreview.fail_on: ")
	builder.WriteString(string(failOn))
	builder.WriteString("\nOnly treat findings at or above this threshold as blocking.")
	builder.WriteString("\n\nAvailable tools:\n")
	for _, definition := range ToolDefinitions() {
		fmt.Fprintf(&builder, "- %s: %s\n", definition.Name, definition.Description)
	}
	builder.WriteString("\nCurrent tasks:\n")
	if len(tasks) == 0 {
		builder.WriteString("- Final pre-archive review of the full change\n")
	} else {
		for _, task := range tasks {
			builder.WriteString("- ")
			builder.WriteString(strings.TrimSpace(task))
			builder.WriteByte('\n')
		}
	}
	builder.WriteString("\nDo not ask for more tools, do not skip review, commit before approving, and approve only with a one-line summary.")
	return strings.TrimSpace(builder.String())
}
