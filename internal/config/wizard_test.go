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
		"poe",
		"GPT-5.4",
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
	if cfg.Controller.Provider != ProviderPoe {
		t.Fatalf("controller provider = %q", cfg.Controller.Provider)
	}
	if cfg.Controller.Model != "GPT-5.4" {
		t.Fatalf("controller model = %q", cfg.Controller.Model)
	}
	if cfg.Controller.MaxToolCalls != 77 {
		t.Fatalf("max_tool_calls = %d", cfg.Controller.MaxToolCalls)
	}
	if cfg.Review.FailOn != FailOnWarning {
		t.Fatalf("fail_on = %q", cfg.Review.FailOn)
	}
	if !strings.Contains(out.String(), "Coding agent (pi|opencode|claude|codex|amp) [pi]: ") {
		t.Fatalf("wizard output missing coding agent prompt: %q", out.String())
	}
	if !strings.Contains(out.String(), "Controller provider (deepseek|openrouter|opencode|poe) [opencode]: ") {
		t.Fatalf("wizard output missing provider prompt: %q", out.String())
	}
}

func TestWizardAmpUsesModePrompts(t *testing.T) {
	input := strings.Join([]string{
		"y",
		"amp",
		"smart",
		"deep",
		"rush",
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
	if cfg.Agent != "amp" {
		t.Fatalf("Agent = %q", cfg.Agent)
	}
	if cfg.Steps.Apply.Model != "smart" || cfg.Steps.Review.Model != "deep" || cfg.Steps.Fix.Model != "rush" {
		t.Fatalf("unexpected amp step values: %#v", cfg.Steps)
	}
	if !strings.Contains(out.String(), "Apply mode (amp --mode: deep|large|rush|smart) [smart]: ") {
		t.Fatalf("wizard output missing amp mode prompt: %q", out.String())
	}
}
