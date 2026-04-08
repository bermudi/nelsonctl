package config

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

type Wizard struct {
	In  io.Reader
	Out io.Writer
}

func (w Wizard) Run(existing Config) (Config, error) {
	reader := bufio.NewReader(w.In)
	cfg := existing
	defaults := DefaultConfig()

	advanced, err := w.askYesNo(reader, "Use advanced setup? [y/N]: ", false)
	if err != nil {
		return Config{}, err
	}
	if !advanced {
		cfg = defaults
		cfg.Controller.Provider = ProviderOpenRouter
		cfg.Controller.Model = "deepseek/deepseek-reasoner"
		return cfg, nil
	}

	agentDefault := fallbackString(existing.Agent, defaults.Agent)
	agentPrompt := fmt.Sprintf("Coding agent (%s) [%s]: ", strings.Join(SupportedAgents(), "|"), agentDefault)
	if cfg.Agent, err = w.askString(reader, agentPrompt, agentDefault); err != nil {
		return Config{}, err
	}
	agentInfo, _ := LookupCodingAgent(cfg.Agent)
	applyModelDefault := NormalizeAgentValue(cfg.Agent, fallbackString(existing.Steps.Apply.Model, defaults.Steps.Apply.Model))
	if cfg.Steps.Apply.Model, err = w.askString(reader, agentValuePrompt("Apply", agentInfo, applyModelDefault), applyModelDefault); err != nil {
		return Config{}, err
	}
	reviewModelDefault := NormalizeAgentValue(cfg.Agent, fallbackString(existing.Steps.Review.Model, defaults.Steps.Review.Model))
	if cfg.Steps.Review.Model, err = w.askString(reader, agentValuePrompt("Review", agentInfo, reviewModelDefault), reviewModelDefault); err != nil {
		return Config{}, err
	}
	fixModelDefault := NormalizeAgentValue(cfg.Agent, fallbackString(existing.Steps.Fix.Model, defaults.Steps.Fix.Model))
	if cfg.Steps.Fix.Model, err = w.askString(reader, agentValuePrompt("Fix", agentInfo, fixModelDefault), fixModelDefault); err != nil {
		return Config{}, err
	}
	applyTimeoutDefault := fallbackDuration(existing.Steps.Apply.Timeout, defaults.Steps.Apply.Timeout)
	if cfg.Steps.Apply.Timeout, err = w.askDuration(reader, promptWithDefault("Apply timeout", applyTimeoutDefault.Std().String()), applyTimeoutDefault); err != nil {
		return Config{}, err
	}
	reviewTimeoutDefault := fallbackDuration(existing.Steps.Review.Timeout, defaults.Steps.Review.Timeout)
	if cfg.Steps.Review.Timeout, err = w.askDuration(reader, promptWithDefault("Review timeout", reviewTimeoutDefault.Std().String()), reviewTimeoutDefault); err != nil {
		return Config{}, err
	}
	fixTimeoutDefault := fallbackDuration(existing.Steps.Fix.Timeout, defaults.Steps.Fix.Timeout)
	if cfg.Steps.Fix.Timeout, err = w.askDuration(reader, promptWithDefault("Fix timeout", fixTimeoutDefault.Std().String()), fixTimeoutDefault); err != nil {
		return Config{}, err
	}

	providerDefault := fallbackString(string(existing.Controller.Provider), string(defaults.Controller.Provider))
	providerPrompt := fmt.Sprintf("Controller provider (%s) [%s]: ", joinControllerProviders(), providerDefault)
	provider, err := w.askString(reader, providerPrompt, providerDefault)
	if err != nil {
		return Config{}, err
	}
	cfg.Controller.Provider = ControllerProvider(provider)
	controllerModelDefault := fallbackString(existing.Controller.Model, defaults.Controller.Model)
	if cfg.Controller.Model, err = w.askString(reader, promptWithDefault("Controller model", controllerModelDefault), controllerModelDefault); err != nil {
		return Config{}, err
	}
	maxToolCallsDefault := fallbackInt(existing.Controller.MaxToolCalls, defaults.Controller.MaxToolCalls)
	if cfg.Controller.MaxToolCalls, err = w.askInt(reader, promptWithDefault("Controller max tool calls", fmt.Sprintf("%d", maxToolCallsDefault)), maxToolCallsDefault); err != nil {
		return Config{}, err
	}
	controllerTimeoutDefault := fallbackDuration(existing.Controller.Timeout, defaults.Controller.Timeout)
	if cfg.Controller.Timeout, err = w.askDuration(reader, promptWithDefault("Controller timeout", controllerTimeoutDefault.Std().String()), controllerTimeoutDefault); err != nil {
		return Config{}, err
	}

	failOnDefault := fallbackString(string(existing.Review.FailOn), string(defaults.Review.FailOn))
	failOn, err := w.askString(reader, promptWithDefault("Review fail_on", failOnDefault), failOnDefault)
	if err != nil {
		return Config{}, err
	}
	cfg.Review.FailOn = ReviewFailOn(failOn)

	cfg.normalize()
	return cfg, cfg.Validate()
}

func (w Wizard) askString(reader *bufio.Reader, prompt, fallback string) (string, error) {
	if _, err := fmt.Fprint(w.Out, prompt); err != nil {
		return "", err
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return fallback, nil
	}
	return value, nil
}

func (w Wizard) askYesNo(reader *bufio.Reader, prompt string, fallback bool) (bool, error) {
	value, err := w.askString(reader, prompt, boolDefault(fallback))
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return fallback, nil
	}
}

func (w Wizard) askDuration(reader *bufio.Reader, prompt string, fallback Duration) (Duration, error) {
	value, err := w.askString(reader, prompt, fallback.Std().String())
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", value, err)
	}
	return Duration(parsed), nil
}

func (w Wizard) askInt(reader *bufio.Reader, prompt string, fallback int) (int, error) {
	value, err := w.askString(reader, prompt, fmt.Sprintf("%d", fallback))
	if err != nil {
		return 0, err
	}
	var out int
	if _, err := fmt.Sscanf(value, "%d", &out); err != nil {
		return 0, fmt.Errorf("parse integer %q: %w", value, err)
	}
	return out, nil
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func fallbackInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func fallbackDuration(value, fallback Duration) Duration {
	if value.Std() > 0 {
		return value
	}
	return fallback
}

func promptWithDefault(label, fallback string) string {
	return fmt.Sprintf("%s [%s]: ", label, fallback)
}

func agentValuePrompt(step string, info CodingAgentInfo, fallback string) string {
	label := step + " " + info.StepValueLabel
	if strings.TrimSpace(info.StepValueLabel) == "" {
		label = step + " model"
	}
	hint := strings.TrimSpace(info.Name)
	if flag := strings.TrimSpace(info.ArgumentFlag); flag != "" {
		hint += " " + flag
	}
	if format := strings.TrimSpace(info.FormatHint); format != "" {
		if hint != "" {
			hint += ": " + format
		} else {
			hint = format
		}
	}
	if hint != "" {
		label += " (" + hint + ")"
	}
	return promptWithDefault(label, fallback)
}

func joinControllerProviders() string {
	providers := make([]string, 0, len(SupportedControllerProviders()))
	for _, provider := range SupportedControllerProviders() {
		providers = append(providers, string(provider))
	}
	return strings.Join(providers, "|")
}

func boolDefault(v bool) string {
	if v {
		return "y"
	}
	return "n"
}
