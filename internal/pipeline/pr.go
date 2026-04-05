package pipeline

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
)

// PullRequestCreator opens a PR or returns manual instructions when gh is unavailable.
type PullRequestCreator interface {
	Create(ctx context.Context, repoDir, title, bodyFile string) (string, error)
}

type GHClient struct {
	lookupPath func(string) (string, error)
	runner     func(ctx context.Context, dir string, name string, args ...string) error
}

// NewGHClient creates a PR creator backed by the gh CLI.
func NewGHClient() *GHClient {
	return &GHClient{
		lookupPath: exec.LookPath,
		runner: func(ctx context.Context, dir string, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err != nil {
				if len(out) > 0 {
					return fmt.Errorf("%s %v failed: %w: %s", name, args, err, string(out))
				}
				return fmt.Errorf("%s %v failed: %w", name, args, err)
			}
			return nil
		},
	}
}

// Create opens a pull request using gh, or returns manual instructions when gh is absent.
func (g *GHClient) Create(ctx context.Context, repoDir, title, bodyFile string) (string, error) {
	lookup := g.lookupPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	if _, err := lookup("gh"); err != nil {
		return fmt.Sprintf("gh not available; run manually: gh pr create --title %q --body-file %s", title, filepath.Base(bodyFile)), nil
	}

	runner := g.runner
	if runner == nil {
		runner = NewGHClient().runner
	}

	if err := runner(ctx, repoDir, "gh", "pr", "create", "--title", title, "--body-file", bodyFile); err != nil {
		return "", err
	}

	return "", nil
}
