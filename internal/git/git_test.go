package git

import (
	"context"
	"reflect"
	"testing"
)

type commandCall struct {
	dir  string
	name string
	args []string
}

type fakeExecutor struct {
	calls []commandCall
	err   error
}

func (f *fakeExecutor) Run(ctx context.Context, dir, name string, args ...string) error {
	f.calls = append(f.calls, commandCall{
		dir:  dir,
		name: name,
		args: append([]string(nil), args...),
	})
	return f.err
}

func TestClientBuildsGitCommands(t *testing.T) {
	fake := &fakeExecutor{}
	client := &Client{Dir: "/repo", Exec: fake}
	ctx := context.Background()

	if err := client.CreateBranch(ctx, "change/initial-scaffold"); err != nil {
		t.Fatalf("CreateBranch() error = %v", err)
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
		{dir: "/repo", name: "git", args: []string{"checkout", "-b", "change/initial-scaffold"}},
		{dir: "/repo", name: "git", args: []string{"add", "--", "specs/changes/initial-scaffold/"}},
		{dir: "/repo", name: "git", args: []string{"commit", "-m", "phase 1: Foundation", "-m", "Initialize the scaffold"}},
		{dir: "/repo", name: "git", args: []string{"push", "-u", "origin", "change/initial-scaffold"}},
	}

	if got := fake.calls; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
}

func TestAddRejectsEmptyPathList(t *testing.T) {
	client := &Client{Dir: "/repo", Exec: &fakeExecutor{}}
	if err := client.Add(context.Background()); err == nil {
		t.Fatal("Add() error = nil, want error")
	}
}
