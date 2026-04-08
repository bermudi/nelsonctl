package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type runCall struct {
	binary            string
	args              []string
	workDir           string
	timeout           time.Duration
	terminationGrace  time.Duration
	stdoutCallbackSet bool
}

func TestNewSelectsSupportedAdapters(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "pi", want: "pi"},
		{name: "opencode", want: "opencode"},
		{name: "claude", want: "claude"},
		{name: "codex", want: "codex"},
		{name: "amp", want: "amp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := New(tt.name)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := agent.Name(); got != tt.want {
				t.Fatalf("Name() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRejectsUnknownAgent(t *testing.T) {
	if _, err := New("unknown"); err == nil {
		t.Fatal("New() error = nil, want error")
	}
}

func TestAdapterCheckPrerequisitesUsesPATHLookup(t *testing.T) {
	a := newCLIAdapter("opencode", "opencode", func(step Step, prompt, model string, format string) []string {
		return []string{"run", "--format", "json", prompt}
	})
	a.lookupPath = func(binary string) (string, error) {
		if binary != "opencode" {
			t.Fatalf("lookupPath binary = %q, want opencode", binary)
		}
		return "", errors.New("not found")
	}

	if err := a.CheckPrerequisites(context.Background()); err == nil {
		t.Fatal("CheckPrerequisites() error = nil, want error")
	}

	a.lookupPath = func(binary string) (string, error) { return "/usr/bin/" + binary, nil }
	if err := a.CheckPrerequisites(context.Background()); err != nil {
		t.Fatalf("CheckPrerequisites() error = %v", err)
	}
}

func TestRunBuildsCommandsAndStreamsStdout(t *testing.T) {
	tests := []struct {
		name     string
		newAgent func(...Option) Agent
		wantArgs []string
		wantName string
	}{
		{
			name:     "opencode",
			newAgent: NewOpencode,
			wantArgs: []string{"run", "--format", "json", "implement phase 2"},
			wantName: "opencode",
		},
		{
			name:     "claude",
			newAgent: NewClaude,
			wantArgs: []string{"-p", "implement phase 2", "--allowedTools", "Bash,Read,Edit", "--output-format", "json"},
			wantName: "claude",
		},
		{
			name:     "codex",
			newAgent: NewCodex,
			wantArgs: []string{"exec", "--json", "implement phase 2"},
			wantName: "codex",
		},
		{
			name:     "amp",
			newAgent: NewAmp,
			wantArgs: []string{"--execute", "--stream-json", "implement phase 2"},
			wantName: "amp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []runCall
			var streamed [][]byte
			agent := tt.newAgent(
				WithTimeout(5*time.Second),
				WithWorkDir("/repo"),
				WithTerminationGracePeriod(250*time.Millisecond),
				WithStdoutCallback(func(chunk []byte) {
					streamed = append(streamed, append([]byte(nil), chunk...))
				}),
			)

			impl := agent.(*cliAdapter)
			impl.lookupPath = func(binary string) (string, error) { return "/usr/bin/" + binary, nil }
			impl.runner = func(ctx context.Context, binary string, args []string, workDir string, timeout time.Duration, terminationGrace time.Duration, stdoutCallback StreamCallback) (*Result, error) {
				calls = append(calls, runCall{
					binary:            binary,
					args:              append([]string(nil), args...),
					workDir:           workDir,
					timeout:           timeout,
					terminationGrace:  terminationGrace,
					stdoutCallbackSet: stdoutCallback != nil,
				})
				if stdoutCallback != nil {
					stdoutCallback([]byte("streamed output"))
				}
				return &Result{Stdout: "done", ExitCode: 0}, nil
			}

			res, err := agent.ExecuteStep(context.Background(), StepApply, "implement phase 2", "")
			if err != nil {
				t.Fatalf("ExecuteStep() error = %v", err)
			}
			if res == nil || res.Stdout != "done" {
				t.Fatalf("Run() result = %#v, want stdout done", res)
			}
			if got, want := len(calls), 1; got != want {
				t.Fatalf("len(calls) = %d, want %d", got, want)
			}
			got := calls[0]
			if got.binary != tt.wantName {
				t.Fatalf("binary = %q, want %q", got.binary, tt.wantName)
			}
			if got.workDir != "/repo" {
				t.Fatalf("workDir = %q, want /repo", got.workDir)
			}
			if got.timeout != 5*time.Second {
				t.Fatalf("timeout = %s, want 5s", got.timeout)
			}
			if got.terminationGrace != 250*time.Millisecond {
				t.Fatalf("terminationGrace = %s, want 250ms", got.terminationGrace)
			}
			if !got.stdoutCallbackSet {
				t.Fatal("stdoutCallbackSet = false, want true")
			}
			if !reflect.DeepEqual(got.args, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", got.args, tt.wantArgs)
			}
			if !reflect.DeepEqual(streamed, [][]byte{[]byte("streamed output")}) {
				t.Fatalf("streamed = %#v, want one stdout chunk", streamed)
			}
		})
	}
}

func TestAmpUsesModeFlagForConfiguredValue(t *testing.T) {
	agent := NewAmp(WithWorkDir("/repo"))
	impl := agent.(*cliAdapter)
	impl.lookupPath = func(binary string) (string, error) { return "/usr/bin/" + binary, nil }

	var gotArgs []string
	impl.runner = func(ctx context.Context, binary string, args []string, workDir string, timeout time.Duration, terminationGrace time.Duration, stdoutCallback StreamCallback) (*Result, error) {
		gotArgs = append([]string(nil), args...)
		return &Result{Stdout: "done", ExitCode: 0}, nil
	}

	if _, err := agent.ExecuteStep(context.Background(), StepApply, "implement phase 2", "smart"); err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	want := []string{"--execute", "--stream-json", "--mode", "smart", "implement phase 2"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args = %#v, want %#v", gotArgs, want)
	}
}
