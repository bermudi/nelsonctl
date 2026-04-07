package pipeline

import (
	"strings"
	"testing"
)

func TestParseTasksMarkdown(t *testing.T) {
	const markdown = "# Tasks\n\n## Phase 1: Foundation\n- [ ] Initialize Go module (`github.com/bermudi/nelsonctl`)\n- [x] Set up project structure (`cmd/nelsonctl/`, `internal/`)\n\n## Phase 2: Agent Adapter\n- [ ] Define `Agent` interface (Name, Available, Run)\n- [X] Implement `opencode` adapter with correct CLI flags\n"

	phases, err := ParseTasksMarkdown(strings.NewReader(markdown))
	if err != nil {
		t.Fatalf("ParseTasksMarkdown() error = %v", err)
	}

	if got, want := len(phases), 2; got != want {
		t.Fatalf("len(phases) = %d, want %d", got, want)
	}

	phase1 := phases[0]
	if phase1.Number != 1 || phase1.Name != "Foundation" {
		t.Fatalf("phase1 = %+v, want number 1 name Foundation", phase1)
	}
	if got, want := len(phase1.Tasks), 2; got != want {
		t.Fatalf("len(phase1.Tasks) = %d, want %d", got, want)
	}
	if phase1.Tasks[0].Done {
		t.Fatalf("phase1.Tasks[0].Done = true, want false")
	}
	if got, want := phase1.Tasks[0].Text, "Initialize Go module (`github.com/bermudi/nelsonctl`)"; got != want {
		t.Fatalf("phase1.Tasks[0].Text = %q, want %q", got, want)
	}
	if !phase1.Tasks[1].Done {
		t.Fatalf("phase1.Tasks[1].Done = false, want true")
	}

	phase2 := phases[1]
	if phase2.Number != 2 || phase2.Name != "Agent Adapter" {
		t.Fatalf("phase2 = %+v, want number 2 name Agent Adapter", phase2)
	}
	if got, want := len(phase2.Tasks), 2; got != want {
		t.Fatalf("len(phase2.Tasks) = %d, want %d", got, want)
	}
	if !phase2.Tasks[1].Done {
		t.Fatalf("phase2.Tasks[1].Done = false, want true")
	}
}

func TestParseTasksMarkdownRejectsTaskBeforePhase(t *testing.T) {
	_, err := ParseTasksMarkdown(strings.NewReader("- [ ] orphan task\n"))
	if err == nil {
		t.Fatal("ParseTasksMarkdown() error = nil, want error")
	}
}

func TestRemainingPhasesStartsAtFirstUncheckedPhase(t *testing.T) {
	phases, err := ParseTasksMarkdown(strings.NewReader("# Tasks\n\n## Phase 1: One\n- [x] done\n\n## Phase 2: Two\n- [ ] next\n\n## Phase 3: Three\n- [ ] later\n"))
	if err != nil {
		t.Fatalf("ParseTasksMarkdown() error = %v", err)
	}
	remaining := RemainingPhases(phases)
	if len(remaining) != 2 {
		t.Fatalf("len(RemainingPhases) = %d, want 2", len(remaining))
	}
	if remaining[0].Number != 2 || remaining[1].Number != 3 {
		t.Fatalf("remaining = %+v", remaining)
	}
	phase, ok := FirstUncheckedPhase(phases)
	if !ok || phase.Number != 2 {
		t.Fatalf("FirstUncheckedPhase() = %+v, %t", phase, ok)
	}
}
