package controller

import (
	"strings"
	"testing"

	"github.com/bermudi/nelsonctl/internal/config"
)

func TestPhaseSystemPromptIncludesThresholdTasksAndTools(t *testing.T) {
	prompt := PhaseSystemPrompt(PhaseRequest{
		ChangeName:   "pi-rpc-integration",
		ReviewFailOn: config.FailOnWarning,
		Phase:        Phase{Number: 2, Name: "Controller AI", Tasks: []string{"Define tools", "Implement loop"}},
	})

	for _, want := range []string{"phase 2 (Controller AI)", "review.fail_on: warning", "Define tools", "Implement loop", "read_file", "get_diff", "submit_prompt", "run_review", "approve"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q in %q", want, prompt)
		}
	}
}

func TestFinalReviewSystemPromptUsesFullChangeContext(t *testing.T) {
	prompt := FinalReviewSystemPrompt(FinalReviewRequest{ChangeName: "pi-rpc-integration", ReviewFailOn: config.FailOnCritical})
	for _, want := range []string{"final pre-archive review controller", "pi-rpc-integration", "review.fail_on: critical", "Final pre-archive review of the full change"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q in %q", want, prompt)
		}
	}
}
