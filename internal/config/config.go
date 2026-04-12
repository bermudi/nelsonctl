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
	ProviderDeepSeek     ControllerProvider = "deepseek"
	ProviderOpenRouter   ControllerProvider = "openrouter"
	ProviderOpenCode     ControllerProvider = "opencode"
	ProviderPoe          ControllerProvider = "poe"
	ProviderPoeResponses ControllerProvider = "poe-responses"
)

type ControllerProviderInfo struct {
	Endpoint       string
	EnvVars        []string
	CredentialHint string
	IsPoeResponses bool
}

var controllerProviderInfo = map[ControllerProvider]ControllerProviderInfo{
	ProviderDeepSeek: {
		Endpoint: "https://api.deepseek.com/chat/completions",
		EnvVars:  []string{"DEEPSEEK_API_KEY"},
	},
	ProviderOpenRouter: {
		Endpoint: "https://openrouter.ai/api/v1/chat/completions",
		EnvVars:  []string{"OPENROUTER_API_KEY"},
	},
	ProviderOpenCode: {
		Endpoint: "https://opencode.ai/zen/go/v1/chat/completions",
		EnvVars:  []string{"OPENCODE_API_KEY"},
	},
	ProviderPoe: {
		Endpoint:       "https://api.poe.com/v1/chat/completions",
		EnvVars:        []string{"POE_API_KEY", "POE_OAUTH_TOKEN", "POE_OAUTH_ACCESS_TOKEN"},
		CredentialHint: "POE_API_KEY or POE_OAUTH_TOKEN",
	},
	ProviderPoeResponses: {
		Endpoint:       "https://api.poe.com/bot/",
		EnvVars:        []string{"POE_API_KEY", "POE_OAUTH_TOKEN", "POE_OAUTH_ACCESS_TOKEN"},
		CredentialHint: "POE_API_KEY or POE_OAUTH_TOKEN",
		IsPoeResponses: true,
	},
}

var controllerProviderAliases = map[ControllerProvider]ControllerProvider{
	"open-code":   ProviderOpenCode,
	"open-router": ProviderOpenRouter,
	"poe.com":     ProviderPoe,
	"poe.responses": ProviderPoeResponses,
}

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
				Timeout: Duration(30 * time.Minute),
			},
		},
		Controller: ControllerConfig{
			Provider:     ProviderOpenCode,
			Model:        "opencode-go/minimax-m2.7",
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
	c.Controller.Provider = NormalizeControllerProvider(c.Controller.Provider)
	c.Controller.Model = strings.TrimSpace(c.Controller.Model)
	c.Review.FailOn = ReviewFailOn(strings.ToLower(strings.TrimSpace(string(c.Review.FailOn))))
	if strings.TrimSpace(c.Steps.Fix.Model) == "" {
		c.Steps.Fix.Model = c.Steps.Apply.Model
	}
}

func (c Config) Validate() error {
	if !supportedAgent(c.Agent) {
		return fmt.Errorf("unsupported agent %q", c.Agent)
	}
	if err := ValidateAgentStepConfig(c, c.Agent); err != nil {
		return err
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

	if _, ok := LookupControllerProvider(c.Controller.Provider); !ok {
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
	_, ok := LookupCodingAgent(name)
	return ok
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

func NormalizeControllerProvider(provider ControllerProvider) ControllerProvider {
	normalized := ControllerProvider(strings.ToLower(strings.TrimSpace(string(provider))))
	if alias, ok := controllerProviderAliases[normalized]; ok {
		return alias
	}
	return normalized
}

func LookupControllerProvider(provider ControllerProvider) (ControllerProviderInfo, bool) {
	info, ok := controllerProviderInfo[NormalizeControllerProvider(provider)]
	return info, ok
}

func SupportedControllerProviders() []ControllerProvider {
	return []ControllerProvider{ProviderDeepSeek, ProviderOpenRouter, ProviderOpenCode, ProviderPoe, ProviderPoeResponses}
}

func RequiredControllerEnvVar(provider ControllerProvider) string {
	envVars := ControllerCredentialEnvVars(provider)
	if len(envVars) == 0 {
		return ""
	}
	return envVars[0]
}

func ControllerCredentialEnvVars(provider ControllerProvider) []string {
	info, ok := LookupControllerProvider(provider)
	if !ok {
		return nil
	}
	return append([]string(nil), info.EnvVars...)
}

func ControllerCredentialHint(provider ControllerProvider) string {
	info, ok := LookupControllerProvider(provider)
	if !ok {
		return ""
	}
	if strings.TrimSpace(info.CredentialHint) != "" {
		return info.CredentialHint
	}
	return joinCredentialEnvVars(info.EnvVars)
}

func ResolveControllerCredential(provider ControllerProvider, getenv func(string) string) (string, string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	info, ok := LookupControllerProvider(provider)
	if !ok {
		return "", "", fmt.Errorf("unsupported controller.provider %q", provider)
	}
	for _, envVar := range info.EnvVars {
		if value := strings.TrimSpace(getenv(envVar)); value != "" {
			return value, envVar, nil
		}
	}
	return "", "", fmt.Errorf("missing required controller credential %s", ControllerCredentialHint(provider))
}

func ValidateControllerCredentials(cfg Config, getenv func(string) string) error {
	_, _, err := ResolveControllerCredential(cfg.Controller.Provider, getenv)
	return err
}

func joinCredentialEnvVars(envVars []string) string {
	switch len(envVars) {
	case 0:
		return ""
	case 1:
		return envVars[0]
	case 2:
		return envVars[0] + " or " + envVars[1]
	default:
		return strings.Join(envVars[:len(envVars)-1], ", ") + ", or " + envVars[len(envVars)-1]
	}
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
