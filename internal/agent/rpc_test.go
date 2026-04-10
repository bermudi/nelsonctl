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

	implEvent := expectTracePayload[SessionCreatedEvent](t, agent.Events())
	if implEvent.SessionID != "impl-1" || implEvent.SessionType != "impl" {
		t.Fatalf("impl session event = %+v", implEvent)
	}

	reviewEvent := expectTracePayload[SessionCreatedEvent](t, agent.Events())
	if reviewEvent.SessionID != "review-1" || reviewEvent.SessionType != "review" || reviewEvent.ParentSession != "/tmp/impl-1.jsonl" {
		t.Fatalf("review session event = %+v", reviewEvent)
	}
}

func TestPiRPCAgentSwitchSessionEmitsTracePayload(t *testing.T) {
	transport := &fakeTransport{
		responses: []rpcResponse{
			{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "impl-1", "sessionFile": "/tmp/impl-1.jsonl"}},
			{Success: true, Command: "switch_session"},
		},
		events: make(chan rpcEvent),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	agent.newClient = func(workDir string, skills []string) rpcTransport { return transport }

	implID, err := agent.StartImplementationSession(context.Background())
	if err != nil {
		t.Fatalf("StartImplementationSession() error = %v", err)
	}
	if implID != "impl-1" {
		t.Fatalf("implID = %q", implID)
	}
	expectTracePayload[SessionCreatedEvent](t, agent.Events())

	if err := agent.switchToSession(context.Background(), "impl-1"); err != nil {
		t.Fatalf("switchToSession() error = %v", err)
	}

	switchedEvent := expectTracePayload[SessionSwitchedEvent](t, agent.Events())
	if switchedEvent.SessionID != "impl-1" {
		t.Fatalf("switched event = %+v", switchedEvent)
	}
	if got, want := transport.commandTypes(), []string{"get_state", "switch_session"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestPiRPCAgentSendMessageModelSetFailureEmitsTracePayloads(t *testing.T) {
	first := &fakeTransport{
		responses: []rpcResponse{
			{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "impl-1", "sessionFile": "/tmp/impl-1.jsonl"}},
			{Success: true, Command: "switch_session"},
		},
		events:  make(chan rpcEvent),
		failAt:  3,
		failErr: fmt.Errorf("model missing"),
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
	expectTracePayload[SessionCreatedEvent](t, agent.Events())

	_, err = agent.SendMessage(context.Background(), "impl-1", "prompt", "opencode-go/kimi-k2.5")
	if err == nil || !strings.Contains(err.Error(), "current phase must be re-run") {
		t.Fatalf("expected restart error, got %v", err)
	}

	modelEvent := expectTracePayloadWhere(t, agent.Events(), func(event ModelSetEvent) bool {
		return event.Provider == "opencode-go" && event.Model == "kimi-k2.5"
	})
	if modelEvent.Success {
		t.Fatalf("model event = %+v", modelEvent)
	}

	restartedEvent := expectTracePayloadWhere(t, agent.Events(), func(event AgentRestartedEvent) bool {
		return event.Cause == "model missing"
	})
	if restartedEvent.Cause != "model missing" {
		t.Fatalf("restarted event = %+v", restartedEvent)
	}

	restartCompletion := expectEventWhere(t, agent.Events(), func(event Event) bool {
		return event.Metadata["restart"] == "true"
	})
	if restartCompletion.Type != CompletionEvent {
		t.Fatalf("restart completion = %+v", restartCompletion)
	}
	if starts != 2 {
		t.Fatalf("starts = %d, want 2", starts)
	}
}

func TestPiRPCAgentForwardEventsEmitsRPCRawTracePayload(t *testing.T) {
	transport := &fakeTransport{events: make(chan rpcEvent, 1)}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	agent.mu.Lock()
	agent.activeSessionID = "impl-1"
	agent.mu.Unlock()

	go func() {
		transport.events <- rpcEvent{Type: "agent_end", Messages: []map[string]any{{"stopReason": "toolUse"}}}
		close(transport.events)
	}()
	go agent.forwardEvents(transport)

	rpcEvent := expectTracePayloadWhere(t, agent.Events(), func(event RPCRawEvent) bool {
		return event.RPCType == "agent_end"
	})
	if rpcEvent.StopReason != "toolUse" || rpcEvent.SessionID != "impl-1" {
		t.Fatalf("rpc event = %+v", rpcEvent)
	}

	assertNoMatchingEvent(t, agent.Events(), 100*time.Millisecond, func(event Event) bool {
		return event.Type == CompletionEvent && event.Metadata["session_id"] == "impl-1"
	})
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
	restartedEvent := expectTracePayloadWhere(t, agent.Events(), func(event AgentRestartedEvent) bool {
		return event.Cause == "broken pipe"
	})
	if restartedEvent.Cause != "broken pipe" {
		t.Fatalf("restart trace payload = %+v", restartedEvent)
	}
	restartCompletion := expectEventWhere(t, agent.Events(), func(event Event) bool {
		return event.Metadata["restart"] == "true"
	})
	if restartCompletion.Type != CompletionEvent {
		t.Fatalf("restart completion = %+v", restartCompletion)
	}
	if starts != 2 {
		t.Fatalf("starts = %d, want 2", starts)
	}
}

func expectAgentEvent(t *testing.T, events <-chan Event) Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("agent event channel closed")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("expected agent event")
		return Event{}
	}
}

func expectTracePayload[T any](t *testing.T, events <-chan Event) T {
	t.Helper()
	return expectTracePayloadWhere(t, events, func(T) bool { return true })
}

func expectEventWhere(t *testing.T, events <-chan Event, match func(Event) bool) Event {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("agent event channel closed")
			}
			if match(event) {
				return event
			}
		case <-deadline:
			t.Fatal("expected matching agent event")
		}
	}
}

func expectTracePayloadWhere[T any](t *testing.T, events <-chan Event, match func(T) bool) T {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatal("agent event channel closed")
			}
			payload, ok := event.TracePayload.(T)
			if ok && match(payload) {
				return payload
			}
		case <-deadline:
			var zero T
			t.Fatal("expected matching trace payload")
			return zero
		}
	}
}

func assertNoMatchingEvent(t *testing.T, events <-chan Event, wait time.Duration, match func(Event) bool) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if match(event) {
				t.Fatalf("unexpected matching agent event: %+v", event)
			}
		case <-deadline:
			return
		}
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
