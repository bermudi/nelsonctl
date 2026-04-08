package config

import "strings"

type CodingAgentInfo struct {
	Name           string
	StepValueLabel string
	ArgumentFlag   string
	FormatHint     string
	DefaultValue   string
}

var codingAgentInfo = map[string]CodingAgentInfo{
	"pi": {
		Name:           "pi",
		StepValueLabel: "model",
		ArgumentFlag:   "--model",
		FormatHint:     `provider/id or pattern[:thinking]`,
	},
	"opencode": {
		Name:           "opencode",
		StepValueLabel: "model",
		ArgumentFlag:   "--model",
		FormatHint:     "provider/model",
	},
	"claude": {
		Name:           "claude",
		StepValueLabel: "model",
		ArgumentFlag:   "--model",
		FormatHint:     "alias or full model name (for example sonnet or claude-sonnet-4-6)",
	},
	"codex": {
		Name:           "codex",
		StepValueLabel: "model",
		ArgumentFlag:   "--model",
		FormatHint:     "model id accepted by codex exec --model",
	},
	"amp": {
		Name:           "amp",
		StepValueLabel: "mode",
		ArgumentFlag:   "--mode",
		FormatHint:     "deep|large|rush|smart",
		DefaultValue:   "smart",
	},
}

func LookupCodingAgent(name string) (CodingAgentInfo, bool) {
	info, ok := codingAgentInfo[strings.ToLower(strings.TrimSpace(name))]
	return info, ok
}

func SupportedAgents() []string {
	return []string{"pi", "opencode", "claude", "codex", "amp"}
}

func CodingAgentValueLabel(agent string) string {
	if info, ok := LookupCodingAgent(agent); ok && info.StepValueLabel != "" {
		return info.StepValueLabel
	}
	return "model"
}

func NormalizeAgentValue(agent, value string) string {
	trimmed := strings.TrimSpace(value)
	info, ok := LookupCodingAgent(agent)
	if !ok {
		return trimmed
	}
	if info.Name == "amp" && !isAmpMode(trimmed) && info.DefaultValue != "" {
		return info.DefaultValue
	}
	return trimmed
}

func ValidateAgentStepConfig(cfg Config, agentName string) error {
	resolved := strings.ToLower(strings.TrimSpace(agentName))
	if !supportedAgent(resolved) {
		return nil
	}
	if resolved != "amp" {
		return nil
	}

	for _, step := range []struct {
		name  string
		value string
	}{
		{name: "apply", value: cfg.Steps.Apply.Model},
		{name: "review", value: cfg.Steps.Review.Model},
		{name: "fix", value: cfg.Steps.Fix.Model},
	} {
		if !isAmpMode(step.value) {
			return ErrInvalidAmpMode(step.name, step.value)
		}
	}
	return nil
}

func isAmpMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "deep", "large", "rush", "smart":
		return true
	default:
		return false
	}
}

func ErrInvalidAmpMode(step, value string) error {
	return &agentConfigError{step: step, value: value}
}

type agentConfigError struct {
	step  string
	value string
}

func (e *agentConfigError) Error() string {
	return "steps." + e.step + ".model must be one of deep, large, rush, or smart when agent is amp (got " + `"` + strings.TrimSpace(e.value) + `"` + ")"
}
