package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRPCClientCorrelatesResponsesAndEvents(t *testing.T) {
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()
	defer stderrWriter.Close()

	client := newRPCClient(t.TempDir(), nil)
	client.starter = func(ctx context.Context, workDir string, env []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
		return &exec.Cmd{}, stdinWriter, stdoutReader, stderrReader, nil
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	go func() {
		scanner := bufio.NewScanner(stdinReader)
		for scanner.Scan() {
			var command rpcCommand
			_ = json.Unmarshal(scanner.Bytes(), &command)
			_, _ = fmt.Fprintf(stdoutWriter, "{\"type\":\"message_update\",\"sessionId\":\"s1\",\"assistantMessageEvent\":{\"part\":{\"text\":\"partial\"}}}\n")
			_, _ = fmt.Fprintf(stdoutWriter, "{\"type\":\"response\",\"id\":%q,\"command\":%q,\"success\":true,\"data\":{\"sessionId\":\"s1\",\"sessionFile\":\"/tmp/s1.jsonl\"}}\n", command.ID, command.Type)
		}
	}()

	response, err := client.Send(context.Background(), rpcCommand{Type: "get_state"})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if response.Command != "get_state" {
		t.Fatalf("response.Command = %q", response.Command)
	}
	select {
	case event := <-client.Events():
		if event.Type != "message_update" {
			t.Fatalf("event.Type = %q", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("expected streamed event")
	}
	_ = stdinWriter.Close()
	_ = stdoutWriter.Close()
}

func TestPiRPCAgentSessionLifecycle(t *testing.T) {
	transport := &fakeTransport{
		responses: []rpcResponse{
			{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "impl-1", "sessionFile": "/tmp/impl-1.jsonl"}},
			{Success: true, Command: "new_session", Data: map[string]any{"cancelled": false}},
			{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "review-1", "sessionFile": "/tmp/review-1.jsonl"}},
			{Success: true, Command: "switch_session"},
		},
		events: make(chan rpcEvent),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	agent.newClient = func(workDir string, skills []string) rpcTransport { return transport }

	impl1, err := agent.StartImplementationSession(context.Background())
	if err != nil {
		t.Fatalf("StartImplementationSession() error = %v", err)
	}
	impl2, err := agent.StartImplementationSession(context.Background())
	if err != nil {
		t.Fatalf("StartImplementationSession() second error = %v", err)
	}
	if impl1 != impl2 || impl1 != "impl-1" {
		t.Fatalf("implementation sessions = %q, %q", impl1, impl2)
	}
	review, err := agent.StartReviewSession(context.Background())
	if err != nil {
		t.Fatalf("StartReviewSession() error = %v", err)
	}
	if review != "review-1" {
		t.Fatalf("review session = %q", review)
	}
	if got, want := transport.commandTypes(), []string{"get_state", "new_session", "get_state", "switch_session"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestPiRPCAgentRestartsAfterCrash(t *testing.T) {
	first := &fakeTransport{
		responses: []rpcResponse{{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "impl-1", "sessionFile": "/tmp/impl-1.jsonl"}}},
		events:    make(chan rpcEvent),
		failAt:    2,
		failErr:   fmt.Errorf("broken pipe"),
	}
	second := &fakeTransport{
		responses: []rpcResponse{{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "impl-2", "sessionFile": "/tmp/impl-2.jsonl"}}},
		events:    make(chan rpcEvent),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	starts := 0
	agent.newClient = func(workDir string, skills []string) rpcTransport {
		starts++
		if starts == 1 {
			return first
		}
		return second
	}

	implID, err := agent.StartImplementationSession(context.Background())
	if err != nil {
		t.Fatalf("StartImplementationSession() error = %v", err)
	}
	if implID != "impl-1" {
		t.Fatalf("implID = %q", implID)
	}
	err = agent.switchToSession(context.Background(), "impl-1")
	if err == nil || !strings.Contains(err.Error(), "current phase must be re-run") {
		t.Fatalf("expected crash restart error, got %v", err)
	}
	if starts != 2 {
		t.Fatalf("starts = %d, want 2", starts)
	}
}

type fakeTransport struct {
	mu        sync.Mutex
	responses []rpcResponse
	commands  []rpcCommand
	events    chan rpcEvent
	failAt    int
	failErr   error
}

func (f *fakeTransport) Start(ctx context.Context) error { return nil }
func (f *fakeTransport) Close() error {
	close(f.events)
	return nil
}
func (f *fakeTransport) Events() <-chan rpcEvent { return f.events }
func (f *fakeTransport) SendNoResponse(command rpcCommand) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, command)
	return nil
}

func (f *fakeTransport) Send(ctx context.Context, command rpcCommand) (rpcResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commands = append(f.commands, command)
	if f.failAt > 0 && len(f.commands) == f.failAt {
		return rpcResponse{}, f.failErr
	}
	if len(f.responses) == 0 {
		return rpcResponse{}, fmt.Errorf("no fake rpc response left for %s", command.Type)
	}
	response := f.responses[0]
	f.responses = f.responses[1:]
	response.Command = command.Type
	response.Success = true
	return response, nil
}

func (f *fakeTransport) commandTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.commands))
	for _, command := range f.commands {
		out = append(out, command.Type)
	}
	return out
}
