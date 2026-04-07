package config

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWizardMinimalSetup(t *testing.T) {
	var out bytes.Buffer
	w := Wizard{In: strings.NewReader("\n"), Out: &out}

	cfg, err := w.Run(DefaultConfig())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if cfg.Agent != "pi" {
		t.Fatalf("Agent = %q, want pi", cfg.Agent)
	}
	if cfg.Controller.Provider != ProviderOpenRouter {
		t.Fatalf("Controller.Provider = %q, want openrouter", cfg.Controller.Provider)
	}
	if cfg.Controller.Model != "deepseek/deepseek-reasoner" {
		t.Fatalf("Controller.Model = %q", cfg.Controller.Model)
	}
}

func TestWizardAdvancedSetup(t *testing.T) {
	input := strings.Join([]string{
		"y",
		"claude",
		"apply/model",
		"review/model",
		"fix/model",
		"12m",
		"8m",
		"14m",
		"openrouter",
		"deepseek/deepseek-reasoner",
		"77",
		"50m",
		"warning",
	}, "\n") + "\n"
	var out bytes.Buffer
	w := Wizard{In: strings.NewReader(input), Out: &out}

	cfg, err := w.Run(DefaultConfig())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if cfg.Agent != "claude" {
		t.Fatalf("Agent = %q", cfg.Agent)
	}
	if cfg.Steps.Apply.Timeout.Std() != 12*time.Minute {
		t.Fatalf("apply timeout = %s", cfg.Steps.Apply.Timeout.Std())
	}
	if cfg.Controller.MaxToolCalls != 77 {
		t.Fatalf("max_tool_calls = %d", cfg.Controller.MaxToolCalls)
	}
	if cfg.Review.FailOn != FailOnWarning {
		t.Fatalf("fail_on = %q", cfg.Review.FailOn)
	}
}
