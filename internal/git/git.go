package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Executor runs a command in a working directory.
type Executor interface {
	Run(ctx context.Context, dir, name string, args ...string) error
	Output(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

// Client wraps git CLI operations for a repository.
type Client struct {
	Dir  string
	Exec Executor
}

// NewClient creates a git client rooted at dir.
func NewClient(dir string) *Client {
	return &Client{Dir: dir}
}

type osExecutor struct{}

func (osExecutor) Run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}

	return nil
}

func (osExecutor) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return nil, fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return nil, fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}

	return out, nil
}

func (c *Client) executor() Executor {
	if c.Exec != nil {
		return c.Exec
	}
	return osExecutor{}
}

// IsClean returns nil when the worktree has no uncommitted changes.
func (c *Client) IsClean(ctx context.Context) error {
	return c.executor().Run(ctx, c.Dir, "git", "diff", "--quiet")
}

// CurrentBranch returns the checked out branch name.
func (c *Client) CurrentBranch(ctx context.Context) (string, error) {
	out, err := c.executor().Output(ctx, c.Dir, "git", "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Diff returns the current uncommitted diff.
func (c *Client) Diff(ctx context.Context) (string, error) {
	out, err := c.executor().Output(ctx, c.Dir, "git", "diff")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ChangedFiles returns paths from git diff --name-only.
func (c *Client) ChangedFiles(ctx context.Context) ([]string, error) {
	out, err := c.executor().Output(ctx, c.Dir, "git", "diff", "--name-only")
	if err != nil {
		return nil, err
	}
	return splitLines(string(out)), nil
}

// HasTrackedChanges reports whether tracked files are modified.
func (c *Client) HasTrackedChanges(ctx context.Context) (bool, error) {
	err := c.executor().Run(ctx, c.Dir, "git", "diff", "--quiet")
	if err == nil {
		return false, nil
	}
	return true, nil
}

// BranchExists reports whether a branch name exists in the local repo.
func (c *Client) BranchExists(ctx context.Context, branch string) (bool, error) {
	exec := c.executor()
	err := exec.Run(ctx, c.Dir, "git", "rev-parse", "--verify", branch)
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "fatal:") || strings.Contains(err.Error(), "Needed") {
		return false, nil
	}
	return false, nil
}

// CreateBranch creates and checks out a new branch.
func (c *Client) CreateBranch(ctx context.Context, branch string) error {
	return c.executor().Run(ctx, c.Dir, "git", "checkout", "-b", branch)
}

// Checkout switches to an existing branch.
func (c *Client) Checkout(ctx context.Context, branch string) error {
	return c.executor().Run(ctx, c.Dir, "git", "checkout", branch)
}

// Add stages one or more paths.
func (c *Client) Add(ctx context.Context, paths ...string) error {
	if len(paths) == 0 {
		return errors.New("git add requires at least one path")
	}

	args := append([]string{"add", "--"}, paths...)
	return c.executor().Run(ctx, c.Dir, "git", args...)
}

// AddAll stages all changes (including untracked files).
func (c *Client) AddAll(ctx context.Context) error {
	return c.executor().Run(ctx, c.Dir, "git", "add", "--all")
}

// Commit creates a commit with subject and optional body.
func (c *Client) Commit(ctx context.Context, subject, body string) error {
	if !c.hasStagedChanges(ctx) {
		return nil
	}
	args := []string{"commit", "-m", subject}
	if body != "" {
		args = append(args, "-m", body)
	}
	return c.executor().Run(ctx, c.Dir, "git", args...)
}

func (c *Client) hasStagedChanges(ctx context.Context) bool {
	err := c.executor().Run(ctx, c.Dir, "git", "diff", "--cached", "--quiet")
	return err != nil
}

// Push pushes the given branch to a remote.
func (c *Client) Push(ctx context.Context, remote, branch string, setUpstream bool) error {
	args := []string{"push"}
	if setUpstream {
		args = append(args, "-u")
	}
	args = append(args, remote, branch)
	return c.executor().Run(ctx, c.Dir, "git", args...)
}

// ProcessExists reports whether pid appears to be alive.
func ProcessExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}

func splitLines(raw string) []string {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
