package git

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type commandCall struct {
	dir  string
	name string
	args []string
}

type fakeExecutor struct {
	calls       []commandCall
	err         error
	byArg       map[string]error
	outputByArg map[string][]byte
}

func (f *fakeExecutor) Run(ctx context.Context, dir, name string, args ...string) error {
	f.calls = append(f.calls, commandCall{
		dir:  dir,
		name: name,
		args: append([]string(nil), args...),
	})
	key := strings.Join(args, " ")
	if f.byArg != nil {
		if err, ok := f.byArg[key]; ok {
			return err
		}
	}
	return f.err
}

func (f *fakeExecutor) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, commandCall{
		dir:  dir,
		name: name,
		args: append([]string(nil), args...),
	})
	key := strings.Join(args, " ")
	if f.byArg != nil {
		if err, ok := f.byArg[key]; ok {
			return nil, err
		}
	}
	if f.outputByArg != nil {
		if out, ok := f.outputByArg[key]; ok {
			return out, nil
		}
	}
	return nil, f.err
}

func TestClientBuildsGitCommands(t *testing.T) {
	fake := &fakeExecutor{
		byArg: map[string]error{
			"rev-parse --verify change/initial-scaffold": errors.New("not found"),
			"diff --cached --quiet":                      errors.New("has staged changes"),
		},
	}
	client := &Client{Dir: "/repo", Exec: fake}
	ctx := context.Background()

	if err := client.IsClean(ctx); err != nil {
		t.Fatalf("IsClean() error = %v", err)
	}
	exists, err := client.BranchExists(ctx, "change/initial-scaffold")
	if err != nil {
		t.Fatalf("BranchExists() error = %v", err)
	}
	if exists {
		t.Fatalf("BranchExists() = true, want false")
	}
	if err := client.CreateBranch(ctx, "change/initial-scaffold"); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
	}
	if err := client.Checkout(ctx, "change/initial-scaffold"); err != nil {
		t.Fatalf("Checkout() error = %v", err)
	}
	if err := client.Add(ctx, "specs/changes/initial-scaffold/"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := client.Commit(ctx, "phase 1: Foundation", "Initialize the scaffold"); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := client.Push(ctx, "origin", "change/initial-scaffold", true); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	want := []commandCall{
		{dir: "/repo", name: "git", args: []string{"diff", "--quiet"}},
		{dir: "/repo", name: "git", args: []string{"rev-parse", "--verify", "change/initial-scaffold"}},
		{dir: "/repo", name: "git", args: []string{"checkout", "-b", "change/initial-scaffold"}},
		{dir: "/repo", name: "git", args: []string{"checkout", "change/initial-scaffold"}},
		{dir: "/repo", name: "git", args: []string{"add", "--", "specs/changes/initial-scaffold/"}},
		{dir: "/repo", name: "git", args: []string{"diff", "--cached", "--quiet"}},
		{dir: "/repo", name: "git", args: []string{"commit", "-m", "phase 1: Foundation", "-m", "Initialize the scaffold"}},
		{dir: "/repo", name: "git", args: []string{"push", "-u", "origin", "change/initial-scaffold"}},
	}

	if got := fake.calls; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
}

func TestClientReportsBranchDiffAndChangedFiles(t *testing.T) {
	fake := &fakeExecutor{outputByArg: map[string][]byte{
		"branch --show-current":     []byte("change/demo\n"),
		"diff":                      []byte("diff --git a/file b/file\n"),
		"diff --name-only":          []byte("file1.go\nfile2.go\n"),
		"diff --cached --name-only": []byte("staged1.go\nstaged2.go\n"),
	}}
	client := &Client{Dir: "/repo", Exec: fake}
	ctx := context.Background()

	branch, err := client.CurrentBranch(ctx)
	if err != nil || branch != "change/demo" {
		t.Fatalf("CurrentBranch() = %q, %v", branch, err)
	}
	diff, err := client.Diff(ctx)
	if err != nil || !strings.Contains(diff, "diff --git") {
		t.Fatalf("Diff() = %q, %v", diff, err)
	}
	files, err := client.ChangedFiles(ctx)
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	if !reflect.DeepEqual(files, []string{"file1.go", "file2.go"}) {
		t.Fatalf("ChangedFiles() = %#v", files)
	}
	staged, err := client.StagedFiles(ctx)
	if err != nil {
		t.Fatalf("StagedFiles() error = %v", err)
	}
	if !reflect.DeepEqual(staged, []string{"staged1.go", "staged2.go"}) {
		t.Fatalf("StagedFiles() = %#v", staged)
	}
}

func TestHasTrackedChanges(t *testing.T) {
	client := &Client{Dir: "/repo", Exec: &fakeExecutor{byArg: map[string]error{"diff --quiet": errors.New("dirty")}}}
	hasChanges, err := client.HasTrackedChanges(context.Background())
	if err != nil {
		t.Fatalf("HasTrackedChanges() error = %v", err)
	}
	if !hasChanges {
		t.Fatal("HasTrackedChanges() = false, want true")
	}
}

func TestAddRejectsEmptyPathList(t *testing.T) {
	client := &Client{Dir: "/repo", Exec: &fakeExecutor{}}
	if err := client.Add(context.Background()); err == nil {
		t.Fatal("Add() error = nil, want error")
	}
}

func TestBranchExistsReturnsTrueWhenBranchPresent(t *testing.T) {
	fake := &fakeExecutor{}
	client := &Client{Dir: "/repo", Exec: fake}
	exists, err := client.BranchExists(context.Background(), "main")
	if err != nil {
		t.Fatalf("BranchExists() error = %v", err)
	}
	if !exists {
		t.Fatal("BranchExists() = false, want true for existing branch")
	}
}

func TestIsCleanReturnsErrorOnDirtyWorktree(t *testing.T) {
	fake := &fakeExecutor{err: errors.New("dirty")}
	client := &Client{Dir: "/repo", Exec: fake}
	if err := client.IsClean(context.Background()); err == nil {
		t.Fatal("IsClean() error = nil, want error for dirty worktree")
	}
}
