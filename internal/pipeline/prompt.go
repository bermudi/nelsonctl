package pipeline

import (
	"fmt"
	"strings"
)

// ApplyPrompt constructs the litespec-apply prompt for a phase.
func ApplyPrompt(changeName string, phase Phase) string {
	return fmt.Sprintf(
		"Use your litespec-apply skill to implement phase %d of change %s. The tasks for this phase are:%s",
		phase.Number,
		changeName,
		phaseTaskLines(phase.Tasks),
	)
}

// ReviewPrompt constructs the litespec-review prompt for a change.
func ReviewPrompt(changeName string) string {
	return fmt.Sprintf(
		"Use your litespec-review skill to review the implementation of change %s.",
		changeName,
	)
}

// FixPrompt constructs the follow-up prompt after a failed review.
func FixPrompt(reviewOutput string) string {
	return fmt.Sprintf(
		"The review found these issues: %s. Fix them.",
		strings.TrimSpace(reviewOutput),
	)
}

// FinalReviewPrompt constructs the final pre-archive review prompt.
func FinalReviewPrompt(changeName string) string {
	return fmt.Sprintf(
		"Use your litespec-review skill in pre-archive mode to review the implementation of change %s.",
		changeName,
	)
}

func phaseTaskLines(tasks []Task) string {
	if len(tasks) == 0 {
		return "\n- (no tasks)"
	}

	var b strings.Builder
	for _, task := range tasks {
		b.WriteString("\n- ")
		b.WriteString(task.Text)
	}
	return b.String()
}
