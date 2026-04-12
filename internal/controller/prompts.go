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
	builder.WriteString(" Drive the implementation loop through tool calls only. Read artifacts on demand with read_file, inspect workspace changes with get_diff, send implementation prompts with submit_prompt, run the mechanical review with run_review, commit changes with commit, and finish only by calling approve. Do not rely on regexes or output formats; reason about review output through comprehension. Only fail the review when issues at or above the configured threshold are present.")
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
	builder.WriteString("\nDo not ask for more tools, do not skip review, and approve only with a one-line summary.")
	return strings.TrimSpace(builder.String())
}
