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
	"sync/atomic"
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

func TestPiRPCAgentExecuteStepSavesSessionPath(t *testing.T) {
	transport := &fakeTransport{
		responses: []rpcResponse{
			{Success: true, Command: "prompt"},
			{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "impl-1", "sessionFile": "/tmp/impl-1.jsonl"}},
		},
		events: make(chan rpcEvent, 8),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	agent.newClient = func(args []string) rpcTransport { return transport }

	// Pre-send completion event into buffered channel
	transport.events <- rpcEvent{
		Type:       "agent_end",
		MessageData: map[string]any{"stopReason": "endTurn"},
	}

	res, err := agent.ExecuteStep(context.Background(), StepApply, "do the thing", "")
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d", res.ExitCode)
	}

	// Verify session path was saved
	agent.mu.Lock()
	path := agent.implSessionPath
	agent.mu.Unlock()
	if path != "/tmp/impl-1.jsonl" {
		t.Fatalf("implSessionPath = %q, want /tmp/impl-1.jsonl", path)
	}

	// Verify commands sent: prompt, then get_state
	if got := transport.commandTypes(); fmt.Sprint(got) != fmt.Sprint([]string{"prompt", "get_state"}) {
		t.Fatalf("commands = %#v", got)
	}

	// Check session created trace event
	implEvent := expectTracePayload[SessionCreatedEvent](t, agent.Events())
	if implEvent.SessionID != "impl-1" || implEvent.SessionType != "impl" {
		t.Fatalf("impl session event = %+v", implEvent)
	}
}

func TestPiRPCAgentExecuteStepWithModel(t *testing.T) {
	transport := &fakeTransport{
		responses: []rpcResponse{
			{Success: true, Command: "set_model"},
			{Success: true, Command: "prompt"},
		},
		events: make(chan rpcEvent, 8),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	agent.newClient = func(args []string) rpcTransport { return transport }

	transport.events <- rpcEvent{
		Type:       "agent_end",
		MessageData: map[string]any{"stopReason": "endTurn"},
	}

	res, err := agent.ExecuteStep(context.Background(), StepApply, "do the thing", "opencode-go/kimi-k2.5")
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode = %d", res.ExitCode)
	}

	// Verify set_model was sent before prompt
	got := transport.commandTypes()
	if len(got) < 2 || got[0] != "set_model" || got[1] != "prompt" {
		t.Fatalf("commands = %#v, want [set_model, prompt]", got)
	}

	modelEvent := expectTracePayloadWhere(t, agent.Events(), func(event ModelSetEvent) bool {
		return event.Provider == "opencode-go" && event.Model == "kimi-k2.5" && event.Success
	})
	if modelEvent.Provider != "opencode-go" {
		t.Fatalf("model event = %+v", modelEvent)
	}
}

func TestPiRPCAgentExecuteStepReviewCreatesChildSession(t *testing.T) {
	implTransport := &fakeTransport{
		responses: []rpcResponse{
			{Success: true, Command: "prompt"},
			{Success: true, Command: "get_state", Data: map[string]any{"sessionId": "impl-1", "sessionFile": "/tmp/impl-1.jsonl"}},
		},
		events: make(chan rpcEvent, 8),
	}
	reviewTransport := &fakeTransport{
		responses: []rpcResponse{
			{Success: true, Command: "new_session"},
			{Success: true, Command: "prompt"},
		},
		events: make(chan rpcEvent, 8),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	callCount := 0
	agent.newClient = func(args []string) rpcTransport {
		callCount++
		if callCount == 1 {
			return implTransport
		}
		return reviewTransport
	}

	// First: apply step (saves impl session path)
	implTransport.events <- rpcEvent{Type: "agent_end", MessageData: map[string]any{"stopReason": "endTurn"}}
	res, err := agent.ExecuteStep(context.Background(), StepApply, "implement", "")
	if err != nil {
		t.Fatalf("apply ExecuteStep() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("apply ExitCode = %d", res.ExitCode)
	}

	// Drain the session created event
	expectTracePayload[SessionCreatedEvent](t, agent.Events())

	// Second: review step (should create child session)
	reviewTransport.events <- rpcEvent{Type: "agent_end", MessageData: map[string]any{"stopReason": "endTurn"}}
	res, err = agent.ExecuteStep(context.Background(), StepReview, "review", "")
	if err != nil {
		t.Fatalf("review ExecuteStep() error = %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("review ExitCode = %d", res.ExitCode)
	}

	// Verify review step sent new_session then prompt
	got := reviewTransport.commandTypes()
	if len(got) < 2 || got[0] != "new_session" || got[1] != "prompt" {
		t.Fatalf("review commands = %#v, want [new_session, prompt]", got)
	}
}

func TestPiRPCAgentExecuteStepPiExits(t *testing.T) {
	transport := &fakeTransport{
		responses: []rpcResponse{},
		events:    make(chan rpcEvent),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	agent.newClient = func(args []string) rpcTransport { return transport }

	// Simulate pi exiting: close events after a brief delay to let processClientEvents start
	go func() {
		time.Sleep(10 * time.Millisecond)
		transport.closeEvents()
	}()

	res, err := agent.ExecuteStep(context.Background(), StepApply, "do thing", "")
	if err != nil {
		t.Fatalf("ExecuteStep() error = %v", err)
	}
	if res.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", res.ExitCode)
	}
}

func TestPiRPCAgentProcessClientEventsEmitsRPCRawTracePayload(t *testing.T) {
	transport := &fakeTransport{events: make(chan rpcEvent, 8)}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)

	var stdout strings.Builder
	done := make(chan error, 1)

	transport.events <- rpcEvent{Type: "agent_end", Messages: []map[string]any{{"stopReason": "toolUse"}}}
	transport.events <- rpcEvent{Type: "agent_end", MessageData: map[string]any{"stopReason": "endTurn"}}

	go agent.processClientEvents(transport, &stdout, done)

	// Should skip toolUse agent_end and only signal on endTurn
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("done error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected completion signal")
	}

	rpcEvent := expectTracePayloadWhere(t, agent.Events(), func(event RPCRawEvent) bool {
		return event.RPCType == "agent_end"
	})
	if rpcEvent.StopReason != "toolUse" {
		t.Fatalf("rpc event stopReason = %q", rpcEvent.StopReason)
	}
}

func TestPiRPCAgentModelSetFailureReturnsError(t *testing.T) {
	transport := &fakeTransport{
		events:  make(chan rpcEvent, 8),
		failAt:  1,
		failErr: fmt.Errorf("model missing"),
	}
	agent := NewPiRPC(WithWorkDir(t.TempDir())).(*piRPCAgent)
	agent.newClient = func(args []string) rpcTransport { return transport }

	res, err := agent.ExecuteStep(context.Background(), StepApply, "prompt", "opencode-go/kimi-k2.5")
	if err == nil {
		t.Fatal("expected error")
	}
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}

	modelEvent := expectTracePayloadWhere(t, agent.Events(), func(event ModelSetEvent) bool {
		return event.Provider == "opencode-go" && event.Model == "kimi-k2.5"
	})
	if modelEvent.Success {
		t.Fatalf("model event should not be success: %+v", modelEvent)
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
	mu         sync.Mutex
	responses  []rpcResponse
	commands   []rpcCommand
	events     chan rpcEvent
	failAt     int
	failErr    error
	eventState int32 // 0=open, 1=closed
}

func (f *fakeTransport) Start(ctx context.Context) error { return nil }
func (f *fakeTransport) Close() error {
	f.closeEvents()
	return nil
}

func (f *fakeTransport) closeEvents() {
	if atomic.CompareAndSwapInt32(&f.eventState, 0, 1) {
		close(f.events)
	}
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
		return rpcResponse{Command: command.Type, Success: true}, nil
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
