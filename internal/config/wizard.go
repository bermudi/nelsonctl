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

	advanced, err := w.askYesNo(reader, "Use advanced setup? [y/N]: ", false)
	if err != nil {
		return Config{}, err
	}
	if !advanced {
		cfg = DefaultConfig()
		cfg.Controller.Provider = ProviderOpenRouter
		cfg.Controller.Model = "deepseek/deepseek-reasoner"
		return cfg, nil
	}

	if cfg.Agent, err = w.askString(reader, "Execution agent [pi]: ", fallbackString(existing.Agent, "pi")); err != nil {
		return Config{}, err
	}
	if cfg.Steps.Apply.Model, err = w.askString(reader, "Apply model [minimax/minimax-m2.7]: ", fallbackString(existing.Steps.Apply.Model, DefaultConfig().Steps.Apply.Model)); err != nil {
		return Config{}, err
	}
	if cfg.Steps.Review.Model, err = w.askString(reader, "Review model [moonshotai/kimi-k2.5]: ", fallbackString(existing.Steps.Review.Model, DefaultConfig().Steps.Review.Model)); err != nil {
		return Config{}, err
	}
	if cfg.Steps.Fix.Model, err = w.askString(reader, "Fix model [minimax/minimax-m2.7]: ", fallbackString(existing.Steps.Fix.Model, DefaultConfig().Steps.Fix.Model)); err != nil {
		return Config{}, err
	}
	if cfg.Steps.Apply.Timeout, err = w.askDuration(reader, "Apply timeout [30m]: ", fallbackDuration(existing.Steps.Apply.Timeout, DefaultConfig().Steps.Apply.Timeout)); err != nil {
		return Config{}, err
	}
	if cfg.Steps.Review.Timeout, err = w.askDuration(reader, "Review timeout [15m]: ", fallbackDuration(existing.Steps.Review.Timeout, DefaultConfig().Steps.Review.Timeout)); err != nil {
		return Config{}, err
	}
	if cfg.Steps.Fix.Timeout, err = w.askDuration(reader, "Fix timeout [30m]: ", fallbackDuration(existing.Steps.Fix.Timeout, DefaultConfig().Steps.Fix.Timeout)); err != nil {
		return Config{}, err
	}

	provider, err := w.askString(reader, "Controller provider [deepseek]: ", fallbackString(string(existing.Controller.Provider), string(DefaultConfig().Controller.Provider)))
	if err != nil {
		return Config{}, err
	}
	cfg.Controller.Provider = ControllerProvider(provider)
	if cfg.Controller.Model, err = w.askString(reader, "Controller model [deepseek-reasoner]: ", fallbackString(existing.Controller.Model, DefaultConfig().Controller.Model)); err != nil {
		return Config{}, err
	}
	if cfg.Controller.MaxToolCalls, err = w.askInt(reader, "Controller max tool calls [50]: ", fallbackInt(existing.Controller.MaxToolCalls, DefaultConfig().Controller.MaxToolCalls)); err != nil {
		return Config{}, err
	}
	if cfg.Controller.Timeout, err = w.askDuration(reader, "Controller timeout [45m]: ", fallbackDuration(existing.Controller.Timeout, DefaultConfig().Controller.Timeout)); err != nil {
		return Config{}, err
	}

	failOn, err := w.askString(reader, "Review fail_on [critical]: ", fallbackString(string(existing.Review.FailOn), string(DefaultConfig().Review.FailOn)))
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

func boolDefault(v bool) string {
	if v {
		return "y"
	}
	return "n"
}
