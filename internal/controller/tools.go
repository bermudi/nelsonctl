package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type DispatchResult struct {
	Content  string
	Approved bool
	Summary  string
}

type Handlers struct {
	RepoDir       string
	ReadFile      func(ctx context.Context, path string) (string, error)
	GetDiff       func(ctx context.Context) (string, error)
	SubmitPrompt  func(ctx context.Context, prompt string) (string, error)
	RunReview     func(ctx context.Context) (string, error)
	Approve       func(ctx context.Context, summary string) error
	AllowAbsolute bool
}

type Dispatcher interface {
	Dispatch(ctx context.Context, call ToolCall) (DispatchResult, error)
}

type ToolDispatcher struct {
	handlers Handlers
}

func NewToolDispatcher(handlers Handlers) *ToolDispatcher {
	if handlers.ReadFile == nil {
		handlers.ReadFile = func(ctx context.Context, path string) (string, error) {
			_ = ctx
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}
	if handlers.GetDiff == nil {
		handlers.GetDiff = func(ctx context.Context) (string, error) {
			cmd := exec.CommandContext(ctx, "git", "diff")
			cmd.Dir = handlers.RepoDir
			out, err := cmd.CombinedOutput()
			if err != nil {
				trimmed := strings.TrimSpace(string(out))
				if trimmed != "" {
					return "", fmt.Errorf("git diff failed: %w: %s", err, trimmed)
				}
				return "", fmt.Errorf("git diff failed: %w", err)
			}
			return string(out), nil
		}
	}
	if handlers.SubmitPrompt == nil {
		handlers.SubmitPrompt = func(ctx context.Context, prompt string) (string, error) {
			_ = ctx
			_ = prompt
			return "Agent completed successfully.", nil
		}
	}
	if handlers.RunReview == nil {
		handlers.RunReview = func(ctx context.Context) (string, error) {
			_ = ctx
			return "", nil
		}
	}
	if handlers.Approve == nil {
		handlers.Approve = func(ctx context.Context, summary string) error {
			_ = ctx
			_ = summary
			return nil
		}
	}

	return &ToolDispatcher{handlers: handlers}
}

func (d *ToolDispatcher) Dispatch(ctx context.Context, call ToolCall) (DispatchResult, error) {
	switch call.Name {
	case ToolReadFile:
		var args ReadFileArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return DispatchResult{}, err
		}
		path, err := d.resolvePath(args.Path)
		if err != nil {
			return DispatchResult{}, err
		}
		content, err := d.handlers.ReadFile(ctx, path)
		if err != nil {
			if os.IsNotExist(err) {
				return DispatchResult{Content: fmt.Sprintf("File %s does not exist.", filepath.ToSlash(args.Path))}, nil
			}
			return DispatchResult{}, fmt.Errorf("read_file %s: %w", args.Path, err)
		}
		return DispatchResult{Content: content}, nil
	case ToolGetDiff:
		var args GetDiffArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return DispatchResult{}, err
		}
		content, err := d.handlers.GetDiff(ctx)
		if err != nil {
			return DispatchResult{}, err
		}
		return DispatchResult{Content: content}, nil
	case ToolSubmitPrompt:
		var args SubmitPromptArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return DispatchResult{}, err
		}
		content, err := d.handlers.SubmitPrompt(ctx, strings.TrimSpace(args.Prompt))
		if err != nil {
			return DispatchResult{}, fmt.Errorf("submit_prompt: %w", err)
		}
		return DispatchResult{Content: content}, nil
	case ToolRunReview:
		var args RunReviewArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return DispatchResult{}, err
		}
		content, err := d.handlers.RunReview(ctx)
		if err != nil {
			return DispatchResult{}, fmt.Errorf("run_review: %w", err)
		}
		return DispatchResult{Content: content}, nil
	case ToolApprove:
		var args ApproveArgs
		if err := decodeArgs(call.Arguments, &args); err != nil {
			return DispatchResult{}, err
		}
		summary := strings.TrimSpace(args.Summary)
		if summary == "" {
			return DispatchResult{}, fmt.Errorf("approve requires a non-empty summary")
		}
		if err := d.handlers.Approve(ctx, summary); err != nil {
			return DispatchResult{}, fmt.Errorf("approve: %w", err)
		}
		return DispatchResult{Content: summary, Approved: true, Summary: summary}, nil
	default:
		return DispatchResult{}, fmt.Errorf("unsupported controller tool %q", call.Name)
	}
}

func (d *ToolDispatcher) resolvePath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("read_file path is required")
	}
	if filepath.IsAbs(trimmed) {
		if !d.handlers.AllowAbsolute {
			return "", fmt.Errorf("absolute read_file paths are not allowed: %s", trimmed)
		}
		return trimmed, nil
	}
	if d.handlers.RepoDir == "" {
		return "", fmt.Errorf("repo dir is required for relative read_file paths")
	}
	return filepath.Join(d.handlers.RepoDir, filepath.Clean(trimmed)), nil
}

func decodeArgs(raw json.RawMessage, target any) error {
	if len(bytesTrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	return nil
}

func bytesTrimSpace(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}
