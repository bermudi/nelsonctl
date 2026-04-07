package controller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolDispatcherReadFileMissingReturnsMessage(t *testing.T) {
	dispatcher := NewToolDispatcher(Handlers{RepoDir: t.TempDir()})
	result, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolReadFile, Arguments: []byte(`{"path":"missing.txt"}`)})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !strings.Contains(result.Content, "does not exist") {
		t.Fatalf("Content = %q", result.Content)
	}
}

func TestToolDispatcherReadFileReturnsContents(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	dispatcher := NewToolDispatcher(Handlers{RepoDir: repo})
	result, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolReadFile, Arguments: []byte(`{"path":"notes.txt"}`)})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("Content = %q", result.Content)
	}
}

func TestToolDispatcherGetDiffDelegates(t *testing.T) {
	called := false
	dispatcher := NewToolDispatcher(Handlers{
		RepoDir: t.TempDir(),
		GetDiff: func(ctx context.Context) (string, error) {
			called = true
			return "diff --git a/file b/file", nil
		},
	})
	result, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolGetDiff, Arguments: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !called || !strings.Contains(result.Content, "diff --git") {
		t.Fatalf("unexpected result: called=%t content=%q", called, result.Content)
	}
}

func TestToolDispatcherSubmitPromptDelegates(t *testing.T) {
	var got string
	dispatcher := NewToolDispatcher(Handlers{
		RepoDir: t.TempDir(),
		SubmitPrompt: func(ctx context.Context, prompt string) (string, error) {
			got = prompt
			return "Agent completed successfully.", nil
		},
	})
	result, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolSubmitPrompt, Arguments: []byte(`{"prompt":"ship it"}`)})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got != "ship it" || result.Content != "Agent completed successfully." {
		t.Fatalf("got=%q content=%q", got, result.Content)
	}
}

func TestToolDispatcherRunReviewDelegates(t *testing.T) {
	dispatcher := NewToolDispatcher(Handlers{
		RepoDir: t.TempDir(),
		RunReview: func(ctx context.Context) (string, error) {
			return "raw review output", nil
		},
	})
	result, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolRunReview, Arguments: []byte(`{}`)})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Content != "raw review output" {
		t.Fatalf("Content = %q", result.Content)
	}
}

func TestToolDispatcherApproveSignalsCompletion(t *testing.T) {
	var approved string
	dispatcher := NewToolDispatcher(Handlers{
		RepoDir: t.TempDir(),
		Approve: func(ctx context.Context, summary string) error {
			approved = summary
			return nil
		},
	})
	result, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolApprove, Arguments: []byte(`{"summary":"all clear"}`)})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !result.Approved || result.Summary != "all clear" || approved != "all clear" {
		t.Fatalf("result=%#v approved=%q", result, approved)
	}
}

func TestToolDispatcherRejectsAbsolutePathByDefault(t *testing.T) {
	dispatcher := NewToolDispatcher(Handlers{RepoDir: t.TempDir()})
	_, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolReadFile, Arguments: []byte(`{"path":"/tmp/file.txt"}`)})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func TestToolDispatcherPropagatesHandlerErrors(t *testing.T) {
	dispatcher := NewToolDispatcher(Handlers{
		RepoDir: t.TempDir(),
		SubmitPrompt: func(ctx context.Context, prompt string) (string, error) {
			return "", errors.New("boom")
		},
	})
	_, err := dispatcher.Dispatch(context.Background(), ToolCall{ID: "1", Name: ToolSubmitPrompt, Arguments: []byte(`{"prompt":"ship it"}`)})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected handler error, got %v", err)
	}
}
