package pipeline

import "fmt"

func MechanicalReviewPrompt(changeName string, final bool) string {
	if final {
		return fmt.Sprintf(
			"Use your litespec-review skill in pre-archive mode to review the full implementation of change %s against the proposal at specs/changes/%s/proposal.md.",
			changeName,
			changeName,
		)
	}
	return fmt.Sprintf(
		"Use your litespec-review skill to review the implementation of change %s against the proposal at specs/changes/%s/proposal.md.",
		changeName,
		changeName,
	)
}
