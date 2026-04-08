package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsWithoutConfigFile(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)

	cfg, path, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if path != filepath.Join(configHome, "nelsonctl", "config.yaml") {
		t.Fatalf("path = %q", path)
	}
	if cfg.Agent != "pi" {
		t.Fatalf("Agent = %q, want pi", cfg.Agent)
	}
	if cfg.Steps.Apply.Model != "minimax/minimax-m2.7" {
		t.Fatalf("apply model = %q", cfg.Steps.Apply.Model)
	}
	if cfg.Controller.Provider != ProviderOpenCode {
		t.Fatalf("provider = %q, want %q", cfg.Controller.Provider, ProviderOpenCode)
	}
	if cfg.Controller.MaxToolCalls != 50 {
		t.Fatalf("max_tool_calls = %d, want 50", cfg.Controller.MaxToolCalls)
	}
	if cfg.Controller.Timeout.Std() != 45*time.Minute {
		t.Fatalf("timeout = %s, want 45m", cfg.Controller.Timeout.Std())
	}
}

func TestLoadParsesConfigFile(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	path := filepath.Join(configHome, "nelsonctl", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := strings.Join([]string{
		"agent: claude",
		"steps:",
		"  apply:",
		"    model: anthropic/claude-sonnet-4",
		"    timeout: 11m",
		"  review:",
		"    model: moonshotai/kimi-k2.5",
		"    timeout: 7m",
		"  fix:",
		"    model: anthropic/claude-sonnet-4",
		"    timeout: 13m",
		"controller:",
		"  provider: openrouter",
		"  model: deepseek/deepseek-reasoner",
		"  max_tool_calls: 64",
		"  timeout: 55m",
		"review:",
		"  fail_on: warning",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Agent != "claude" {
		t.Fatalf("Agent = %q", cfg.Agent)
	}
	if cfg.Steps.Apply.Timeout.Std() != 11*time.Minute {
		t.Fatalf("apply timeout = %s", cfg.Steps.Apply.Timeout.Std())
	}
	if cfg.Controller.Provider != ProviderOpenRouter {
		t.Fatalf("provider = %q", cfg.Controller.Provider)
	}
	if cfg.Review.FailOn != FailOnWarning {
		t.Fatalf("fail_on = %q", cfg.Review.FailOn)
	}
}

func TestWriteOmitsCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := DefaultConfig()
	if err := Write(path, cfg); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	text := string(data)
	for _, forbidden := range []string{"DEEPSEEK_API_KEY", "OPENROUTER_API_KEY", "POE_API_KEY", "POE_OAUTH_TOKEN", "api_key:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("config should not contain %q", forbidden)
		}
	}
}

func TestValidateControllerCredentials(t *testing.T) {
	cfg := DefaultConfig()
	if err := ValidateControllerCredentials(cfg, func(key string) string { return "" }); err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY") {
		t.Fatalf("expected missing OPENROUTER_API_KEY, got %v", err)
	}

	cfg.Controller.Provider = ProviderOpenRouter
	if err := ValidateControllerCredentials(cfg, func(key string) string {
		if key == "OPENROUTER_API_KEY" {
			return "present"
		}
		return ""
	}); err != nil {
		t.Fatalf("ValidateControllerCredentials() error = %v", err)
	}

	cfg.Controller.Provider = ProviderPoe
	if err := ValidateControllerCredentials(cfg, func(key string) string {
		if key == "POE_API_KEY" {
			return "present"
		}
		return ""
	}); err != nil {
		t.Fatalf("ValidateControllerCredentials() poe api key error = %v", err)
	}
	if err := ValidateControllerCredentials(cfg, func(key string) string {
		if key == "POE_OAUTH_TOKEN" {
			return "present"
		}
		return ""
	}); err != nil {
		t.Fatalf("ValidateControllerCredentials() poe oauth error = %v", err)
	}
}

func TestResolveControllerCredential(t *testing.T) {
	value, source, err := ResolveControllerCredential(ProviderPoe, func(key string) string {
		switch key {
		case "POE_OAUTH_TOKEN":
			return "oauth-token"
		case "POE_API_KEY":
			return ""
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("ResolveControllerCredential() error = %v", err)
	}
	if value != "oauth-token" || source != "POE_OAUTH_TOKEN" {
		t.Fatalf("ResolveControllerCredential() = %q, %q", value, source)
	}

	_, _, err = ResolveControllerCredential(ProviderPoe, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "POE_OAUTH_TOKEN") {
		t.Fatalf("expected oauth hint, got %v", err)
	}
}

func TestValidateAgentStepConfigAmp(t *testing.T) {
	cfg := DefaultConfig()
	if err := ValidateAgentStepConfig(cfg, "pi"); err != nil {
		t.Fatalf("ValidateAgentStepConfig(pi) error = %v", err)
	}
	if err := ValidateAgentStepConfig(cfg, "amp"); err == nil || !strings.Contains(err.Error(), "deep, large, rush, or smart") {
		t.Fatalf("expected amp mode validation error, got %v", err)
	}

	cfg.Agent = "amp"
	cfg.Steps.Apply.Model = "smart"
	cfg.Steps.Review.Model = "deep"
	cfg.Steps.Fix.Model = "rush"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg.Validate() amp error = %v", err)
	}
}

func TestNormalizeControllerProviderAliases(t *testing.T) {
	if got := NormalizeControllerProvider("open-router"); got != ProviderOpenRouter {
		t.Fatalf("NormalizeControllerProvider(open-router) = %q", got)
	}
	if got := NormalizeControllerProvider("open-code"); got != ProviderOpenCode {
		t.Fatalf("NormalizeControllerProvider(open-code) = %q", got)
	}
	if got := NormalizeControllerProvider("poe.com"); got != ProviderPoe {
		t.Fatalf("NormalizeControllerProvider(poe.com) = %q", got)
	}
}

func TestResolveAgentPiFirst(t *testing.T) {
	cfg := DefaultConfig()
	resolved := ResolveAgent(cfg, "")
	if resolved.Name != "pi" || resolved.Mode != ModeNelson || resolved.Source != "config" {
		t.Fatalf("resolved = %#v", resolved)
	}

	resolved = ResolveAgent(cfg, "codex")
	if resolved.Name != "codex" || resolved.Mode != ModeRalph || resolved.Source != "flag" {
		t.Fatalf("resolved override = %#v", resolved)
	}
}

func TestValidateWorkspace(t *testing.T) {
	repo := t.TempDir()
	if err := ValidateWorkspace(repo); err == nil || !strings.Contains(err.Error(), "specs/") {
		t.Fatalf("expected specs validation error, got %v", err)
	}

	paths := []string{
		filepath.Join(repo, "specs"),
		filepath.Join(repo, ".agents", "skills", "litespec-apply"),
		filepath.Join(repo, ".agents", "skills", "litespec-review"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	for _, file := range []string{
		filepath.Join(repo, ".agents", "skills", "litespec-apply", "SKILL.md"),
		filepath.Join(repo, ".agents", "skills", "litespec-review", "SKILL.md"),
	} {
		if err := os.WriteFile(file, []byte("skill"), 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}

	if err := ValidateWorkspace(repo); err != nil {
		t.Fatalf("ValidateWorkspace() error = %v", err)
	}
}
