package pipeline

import (
	"fmt"
	"strings"
)

// ApplyPrompt constructs the apply prompt for a phase.
func ApplyPrompt(changeName string, phase Phase) string {
	return fmt.Sprintf(
		"Implement phase %d of change %s. The tasks for this phase are:%s",
		phase.Number,
		changeName,
		phaseTaskLines(phase.Tasks),
	)
}

// ReviewPrompt constructs the review prompt for a change.
func ReviewPrompt(changeName string) string {
	return fmt.Sprintf(
		"Review the implementation of change %s. Report whether it is complete and correct, or list specific issues.",
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
		"Do a final review of the full implementation of change %s. Confirm everything is complete and ready to archive, or list remaining issues.",
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
