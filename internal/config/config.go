package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

func (d Duration) MarshalYAML() (any, error) {
	return d.Std().String(), nil
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}
	*d = Duration(parsed)
	return nil
}

type ControllerProvider string

const (
	ProviderDeepSeek   ControllerProvider = "deepseek"
	ProviderOpenRouter ControllerProvider = "openrouter"
)

type ReviewFailOn string

const (
	FailOnCritical   ReviewFailOn = "critical"
	FailOnWarning    ReviewFailOn = "warning"
	FailOnSuggestion ReviewFailOn = "suggestion"
)

type ExecutionMode string

const (
	ModeNelson ExecutionMode = "Nelson"
	ModeRalph  ExecutionMode = "Ralph"
)

type StepConfig struct {
	Model   string   `yaml:"model"`
	Timeout Duration `yaml:"timeout"`
}

type StepsConfig struct {
	Apply  StepConfig `yaml:"apply"`
	Review StepConfig `yaml:"review"`
	Fix    StepConfig `yaml:"fix"`
}

type ControllerConfig struct {
	Provider     ControllerProvider `yaml:"provider"`
	Model        string             `yaml:"model"`
	MaxToolCalls int                `yaml:"max_tool_calls"`
	Timeout      Duration           `yaml:"timeout"`
}

type ReviewConfig struct {
	FailOn ReviewFailOn `yaml:"fail_on"`
}

type Config struct {
	Agent      string           `yaml:"agent"`
	Steps      StepsConfig      `yaml:"steps"`
	Controller ControllerConfig `yaml:"controller"`
	Review     ReviewConfig     `yaml:"review"`
}

type ResolvedAgent struct {
	Name   string
	Mode   ExecutionMode
	Source string
}

func DefaultConfig() Config {
	return Config{
		Agent: "pi",
		Steps: StepsConfig{
			Apply: StepConfig{
				Model:   "minimax/minimax-m2.7",
				Timeout: Duration(30 * time.Minute),
			},
			Review: StepConfig{
				Model:   "moonshotai/kimi-k2.5",
				Timeout: Duration(15 * time.Minute),
			},
			Fix: StepConfig{
				Model:   "minimax/minimax-m2.7",
				Timeout: Duration(30 * time.Minute),
			},
		},
		Controller: ControllerConfig{
			Provider:     ProviderDeepSeek,
			Model:        "deepseek-reasoner",
			MaxToolCalls: 50,
			Timeout:      Duration(45 * time.Minute),
		},
		Review: ReviewConfig{FailOn: FailOnCritical},
	}
}

func ConfigPath() (string, error) {
	if base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); base != "" {
		return filepath.Join(base, "nelsonctl", "config.yaml"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "nelsonctl", "config.yaml"), nil
}

func Load() (Config, string, error) {
	path, err := ConfigPath()
	if err != nil {
		return Config{}, "", err
	}

	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, path, nil
		}
		return Config{}, path, fmt.Errorf("read config %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, path, fmt.Errorf("parse config %s: %w", path, err)
	}

	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, path, err
	}

	return cfg, path, nil
}

func Write(path string, cfg Config) error {
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write config %s: %w", path, err)
	}

	return nil
}

func (c *Config) normalize() {
	c.Agent = strings.ToLower(strings.TrimSpace(c.Agent))
	c.Steps.Apply.Model = strings.TrimSpace(c.Steps.Apply.Model)
	c.Steps.Review.Model = strings.TrimSpace(c.Steps.Review.Model)
	c.Steps.Fix.Model = strings.TrimSpace(c.Steps.Fix.Model)
	c.Controller.Provider = ControllerProvider(strings.ToLower(strings.TrimSpace(string(c.Controller.Provider))))
	c.Controller.Model = strings.TrimSpace(c.Controller.Model)
	c.Review.FailOn = ReviewFailOn(strings.ToLower(strings.TrimSpace(string(c.Review.FailOn))))
}

func (c Config) Validate() error {
	if !supportedAgent(c.Agent) {
		return fmt.Errorf("unsupported agent %q", c.Agent)
	}

	if err := validateStep("apply", c.Steps.Apply); err != nil {
		return err
	}
	if err := validateStep("review", c.Steps.Review); err != nil {
		return err
	}
	if err := validateStep("fix", c.Steps.Fix); err != nil {
		return err
	}

	switch c.Controller.Provider {
	case ProviderDeepSeek, ProviderOpenRouter:
	default:
		return fmt.Errorf("unsupported controller.provider %q", c.Controller.Provider)
	}
	if c.Controller.Model == "" {
		return fmt.Errorf("controller.model is required")
	}
	if c.Controller.MaxToolCalls <= 0 {
		return fmt.Errorf("controller.max_tool_calls must be greater than zero")
	}
	if c.Controller.Timeout.Std() <= 0 {
		return fmt.Errorf("controller.timeout must be greater than zero")
	}

	switch c.Review.FailOn {
	case FailOnCritical, FailOnWarning, FailOnSuggestion:
	default:
		return fmt.Errorf("unsupported review.fail_on %q", c.Review.FailOn)
	}

	return nil
}

func validateStep(name string, step StepConfig) error {
	if step.Model == "" {
		return fmt.Errorf("steps.%s.model is required", name)
	}
	if step.Timeout.Std() <= 0 {
		return fmt.Errorf("steps.%s.timeout must be greater than zero", name)
	}
	return nil
}

func supportedAgent(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "pi", "opencode", "claude", "codex", "amp":
		return true
	default:
		return false
	}
}

func ResolveAgent(cfg Config, cliAgent string) ResolvedAgent {
	requested := strings.ToLower(strings.TrimSpace(cliAgent))
	source := "flag"
	if requested == "" {
		requested = strings.ToLower(strings.TrimSpace(cfg.Agent))
		source = "config"
	}
	if requested == "" {
		requested = "pi"
		source = "default"
	}

	mode := ModeRalph
	if requested == "pi" {
		mode = ModeNelson
	}

	return ResolvedAgent{Name: requested, Mode: mode, Source: source}
}

func (c Config) EffectiveTimeout() time.Duration {
	max := c.Steps.Apply.Timeout.Std()
	if review := c.Steps.Review.Timeout.Std(); review > max {
		max = review
	}
	if fix := c.Steps.Fix.Timeout.Std(); fix > max {
		max = fix
	}
	return max
}

func RequiredControllerEnvVar(provider ControllerProvider) string {
	switch provider {
	case ProviderDeepSeek:
		return "DEEPSEEK_API_KEY"
	case ProviderOpenRouter:
		return "OPENROUTER_API_KEY"
	default:
		return ""
	}
}

func ValidateControllerCredentials(cfg Config, getenv func(string) string) error {
	if getenv == nil {
		getenv = os.Getenv
	}

	required := RequiredControllerEnvVar(cfg.Controller.Provider)
	if required == "" {
		return fmt.Errorf("unsupported controller.provider %q", cfg.Controller.Provider)
	}
	if strings.TrimSpace(getenv(required)) == "" {
		return fmt.Errorf("missing required controller credential %s", required)
	}
	return nil
}

func ValidateWorkspace(repoDir string) error {
	checks := []struct {
		path string
		msg  string
	}{
		{path: filepath.Join(repoDir, "specs"), msg: "missing specs/ directory; run nelsonctl from a litespec workspace root"},
		{path: filepath.Join(repoDir, ".agents", "skills", "litespec-apply", "SKILL.md"), msg: "missing required skill .agents/skills/litespec-apply/SKILL.md"},
		{path: filepath.Join(repoDir, ".agents", "skills", "litespec-review", "SKILL.md"), msg: "missing required skill .agents/skills/litespec-review/SKILL.md"},
	}

	for _, check := range checks {
		if _, err := os.Stat(check.path); err != nil {
			if os.IsNotExist(err) {
				return errors.New(check.msg)
			}
			return fmt.Errorf("check %s: %w", check.path, err)
		}
	}

	return nil
}
