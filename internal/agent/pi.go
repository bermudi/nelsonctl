package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// piRPCAgent implements the per-step RPC agent for Pi.
// Each ExecuteStep call starts a fresh pi process, runs one command, and shuts it down.
// Implementation sessions are resumed via --session; review sessions are created fresh
// with the impl session as parent for context.
type piRPCAgent struct {
	settings        settings
	lookupPath      func(string) (string, error)
	implSessionPath string // persisted session file for apply/fix steps
	events          *queuedChannel[Event]
	mu              sync.Mutex
	closed          bool
	newClient       func(args []string) rpcTransport
}

func NewPiRPC(opts ...Option) Agent {
	settings := defaultSettings()
	for _, opt := range opts {
		opt(&settings)
	}
	if settings.workDir == "" {
		settings.workDir = "."
	}
	agent := &piRPCAgent{
		settings:   settings,
		lookupPath: nil,
		events:     newQueuedChannel[Event](settings.eventBufferSize()),
	}
	agent.newClient = agent.defaultNewClient
	return agent
}

func (a *piRPCAgent) Name() string {
	return "pi"
}

func (a *piRPCAgent) CheckPrerequisites(ctx context.Context) error {
	return checkBinaryPrerequisites(ctx, "pi", a.lookupPath)
}

// ExecuteStep starts a fresh pi process, runs one agent step, and shuts it down.
// For apply/fix steps, the implementation session is resumed via --session.
// For review steps, a new child session is created with the impl session as parent.
func (a *piRPCAgent) ExecuteStep(ctx context.Context, step Step, prompt, model string) (*Result, error) {
	// Determine extra args for this step
	var extraArgs []string
	a.mu.Lock()
	implPath := a.implSessionPath
	a.mu.Unlock()
	if (step == StepApply || step == StepFix) && implPath != "" {
		extraArgs = []string{"--session", implPath}
	}

	// Start a fresh pi process
	client := a.newClient(extraArgs)
	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("start pi for %s: %w", step, err)
	}

	// Set up event processing: reads from client, collects stdout, signals completion
	var stdout strings.Builder
	done := make(chan error, 1)
	go a.processClientEvents(client, &stdout, done)

	// For review steps, create a child session with impl session as parent for context
	if (step == StepReview || step == StepFinalReview) && implPath != "" {
		if _, err := client.Send(ctx, rpcCommand{Type: "new_session", ParentSession: implPath}); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("create review session: %w", err)
		}
	}

	// Set model if specified
	resolvedModel := pickModel(model, a.settings.modelFor(step))
	if resolvedModel != "" {
		provider, modelID := splitModel(resolvedModel)
		if provider != "" && modelID != "" {
			if _, err := client.Send(ctx, rpcCommand{Type: "set_model", Provider: provider, ModelID: modelID}); err != nil {
				a.emit(Event{TracePayload: ModelSetEvent{Provider: provider, Model: modelID, Success: false}})
				_ = client.Close()
				return nil, fmt.Errorf("set model %s: %w", resolvedModel, err)
			}
			a.emit(Event{TracePayload: ModelSetEvent{Provider: provider, Model: modelID, Success: true}})
		}
	}

	// Send prompt
	start := time.Now()
	trimmedPrompt := strings.TrimSpace(prompt)
	a.emit(Event{TracePayload: CommandStartEvent{Name: "prompt", Command: "prompt", WorkDir: a.settings.workDirectory(), Source: "pi_rpc"}})

	if _, err := client.Send(ctx, rpcCommand{Type: "prompt", Message: trimmedPrompt}); err != nil {
		a.emit(Event{TracePayload: CommandResultEvent{Name: "prompt", Command: "prompt", ExitCode: 1, DurationMs: time.Since(start).Milliseconds(), Error: err.Error(), Source: "pi_rpc"}})
		_ = client.Close()
		return nil, fmt.Errorf("send prompt: %w", err)
	}

	// Wait for agent completion with optional per-step timeout
	stepCtx := ctx
	if timeout := a.settings.timeoutFor(step); timeout > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	select {
	case err := <-done:
		res := &Result{Stdout: stdout.String(), ExitCode: 0, Duration: time.Since(start)}
		if err != nil {
			res.ExitCode = 1
			a.emit(Event{TracePayload: CommandResultEvent{Name: "prompt", Command: "prompt", ExitCode: 1, DurationMs: res.Duration.Milliseconds(), Error: err.Error(), Source: "pi_rpc"}})
		} else {
			a.emit(Event{TracePayload: CommandResultEvent{Name: "prompt", Command: "prompt", ExitCode: 0, DurationMs: res.Duration.Milliseconds(), Source: "pi_rpc"}})
		}

		// After first successful apply, persist the session file path for future fix steps
		if err == nil && step == StepApply {
			a.saveImplSessionPath(ctx, client)
		}

		_ = client.Close()
		return res, nil

	case <-stepCtx.Done():
		_ = client.SendNoResponse(rpcCommand{Type: "abort"})
		_ = client.Close()
		return &Result{ExitCode: 1, Duration: time.Since(start)}, stepCtx.Err()
	}
}

// saveImplSessionPath queries the pi process for its session file path and persists it.
// This is called after the first successful apply step so that subsequent fix steps
// can resume the session via --session.
func (a *piRPCAgent) saveImplSessionPath(ctx context.Context, client rpcTransport) {
	a.mu.Lock()
	if a.implSessionPath != "" {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	resp, err := client.Send(ctx, rpcCommand{Type: "get_state"})
	if err != nil {
		return
	}
	state, err := decodeState(resp)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.implSessionPath = state.SessionFile
	a.mu.Unlock()
	a.emit(Event{TracePayload: SessionCreatedEvent{SessionID: state.SessionID, SessionType: "impl"}})
}

func (a *piRPCAgent) Cleanup(ctx context.Context) error {
	_ = ctx
	return a.Close()
}

func (a *piRPCAgent) AsRPC() RPCAgent {
	return a
}

func (a *piRPCAgent) Events() <-chan Event {
	return a.events.Channel()
}

// StartImplementationSession returns the implementation session ID.
// In per-step mode, the session is created on the first apply step;
// this just returns whatever we already know.
func (a *piRPCAgent) StartImplementationSession(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.implSessionPath, nil
}

// StartReviewSession returns a review session ID.
// In per-step mode, review sessions are created inside ExecuteStep;
// this is a no-op that returns empty.
func (a *piRPCAgent) StartReviewSession(ctx context.Context) (string, error) {
	return "", nil
}

// SendMessage is not used in per-step mode; ExecuteStep handles everything.
func (a *piRPCAgent) SendMessage(ctx context.Context, sessionID, prompt, model string) (*Result, error) {
	return nil, fmt.Errorf("SendMessage not supported in per-step mode; use ExecuteStep")
}

func (a *piRPCAgent) Abort(ctx context.Context, sessionID string) error {
	// In per-step mode, abort is handled by closing the client in ExecuteStep
	return nil
}

func (a *piRPCAgent) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	a.mu.Unlock()
	a.events.Close()
	return nil
}

func (a *piRPCAgent) SessionForStep(ctx context.Context, step Step) (string, error) {
	if step == StepApply || step == StepFix {
		a.mu.Lock()
		path := a.implSessionPath
		a.mu.Unlock()
		return path, nil
	}
	return "", nil
}

// processClientEvents reads events from a pi RPC client, collects text output,
// forwards trace events, and signals completion via the done channel.
// This replaces the old forwardEvents + consumeSessionEvents pair.
func (a *piRPCAgent) processClientEvents(client rpcTransport, stdout *strings.Builder, done chan<- error) {
	for event := range client.Events() {
		a.emitRPCSummary(event)

		switch event.Type {
		case "extension_ui_request":
			if event.ID != "" {
				_ = client.SendNoResponse(rpcCommand{Type: "extension_ui_response", ID: event.ID, Cancelled: true})
			}
		case "message_update":
			content := event.Text
			if event.AssistantUpdate != nil {
				switch event.AssistantUpdate.Type {
				case "text_delta", "text_start":
					if event.AssistantUpdate.Delta != "" {
						content = event.AssistantUpdate.Delta
					}
					if event.AssistantUpdate.Part.Text != "" {
						content = event.AssistantUpdate.Part.Text
					}
				default:
					// thinking_*, toolcall_*, done, error, text_end — no visible text
					content = ""
				}
			}
			if content != "" {
				stdout.WriteString(content)
				a.emit(Event{Type: TextEvent, Content: content})
			}
		case "agent_end":
			stopReason := stopReasonFromRPCEvent(event)
			// Pi fires agent_end at the end of every conversation turn.
			// When stopReason is "toolUse", the agent is pausing to execute
			// tool calls — it will continue automatically. Only treat the
			// agent as truly done when the stop reason is something else
			// (e.g. "endTurn" or empty).
			if stopReason == "toolUse" {
				if os.Getenv("NELSONCTL_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[DEBUG-agent] agent_end with toolUse — ignoring (agent will continue)\n")
				}
				continue
			}
			done <- nil
			return
		case "tool_execution_start":
			a.emitToolExecutionStart(event)
		case "tool_execution_end":
			a.emitToolExecutionResult(event)
		case "extension_error":
			a.emit(Event{Type: ErrorEvent, Content: event.Error})
			done <- errors.New(event.Error)
			return
		}
	}
	// Events channel closed — pi exited
	done <- fmt.Errorf("pi process exited")
}

func (a *piRPCAgent) defaultNewClient(extraArgs []string) rpcTransport {
	skills, _ := discoverWorkspaceSkills(a.settings.workDirectory())
	args := []string{"--mode", "rpc", "--no-extensions"}
	args = append(args, extraArgs...)
	for _, skill := range skills {
		args = append(args, "--skill", skill)
	}
	c := newRPCClient(a.settings.workDirectory(), nil)
	c.starter = func(ctx context.Context, workDir string, env []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
		return startPiRPCProcessWithArgs(ctx, workDir, env, args)
	}
	return c
}

func (a *piRPCAgent) emit(event Event) {
	a.events.Send(event)
}

func (a *piRPCAgent) emitRPCSummary(event rpcEvent) {
	payload, err := summarizeRPCPayload(event)
	if err != nil {
		a.emit(Event{TracePayload: RPCRawEvent{RPCType: event.Type, StopReason: "error", PayloadSummary: fmt.Sprintf("failed to summarize payload: %v", err)}})
		return
	}
	stopReason := ""
	if event.Type == "agent_end" {
		stopReason = stopReasonFromRPCEvent(event)
	}
	a.emit(Event{TracePayload: RPCRawEvent{RPCType: event.Type, StopReason: stopReason, PayloadSummary: rpcPayloadSummary(event), Payload: payload}})
}

func (a *piRPCAgent) emitToolExecutionStart(event rpcEvent) {
	_, commandText, commandArgs := commandDetailsFromToolEvent(event)
	a.emit(Event{TracePayload: CommandStartEvent{
		Name:            event.Type,
		Command:         commandText,
		Args:            commandArgs,
		WorkDir:         a.settings.workDirectory(),
		Source:          "pi_rpc",
		ToolCallID:      event.ToolCallID,
		ToolName:        event.ToolName,
		ProviderSummary: providerSummaryFromToolEvent(event),
	}})
}

func (a *piRPCAgent) emitToolExecutionResult(event rpcEvent) {
	commandName, commandText, commandArgs := commandDetailsFromToolEvent(event)
	stdoutLen, stderrLen, exitCode := toolResultSummary(event)
	a.emit(Event{TracePayload: CommandResultEvent{
		Name:        event.Type,
		Command:     commandText,
		ExitCode:    exitCode,
		StdoutLen:   stdoutLen,
		StderrLen:   stderrLen,
		Error:       errorTextFromToolEvent(event),
		Source:      "pi_rpc",
		ToolCallID:  event.ToolCallID,
		ToolName:    event.ToolName,
		CommandName: commandName,
		CommandArgs: commandArgs,
	}})
}

func pickModel(requested, fallback string) string {
	if strings.TrimSpace(requested) != "" {
		return strings.TrimSpace(requested)
	}
	return strings.TrimSpace(fallback)
}

func splitModel(model string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(model), "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func stopReasonFromRPCEvent(event rpcEvent) string {
	if stopReason, ok := extractStopReason(event.MessageData); ok {
		return stopReason
	}
	// agent_end contains ALL conversation messages. Earlier assistant turns
	// may have stopReason "toolUse", but the LAST message's stopReason is the
	// one that matters — it tells us why the agent finally stopped.
	var last string
	for _, message := range event.Messages {
		if sr, ok := extractStopReason(message); ok {
			last = sr
		}
	}
	if last != "" {
		return last
	}
	return ""
}

func extractStopReason(message map[string]any) (string, bool) {
	if len(message) == 0 {
		return "", false
	}
	if stopReason, ok := message["stopReason"].(string); ok && strings.TrimSpace(stopReason) != "" {
		return stopReason, true
	}
	if stopReason, ok := message["stop_reason"].(string); ok && strings.TrimSpace(stopReason) != "" {
		return stopReason, true
	}
	if nested, ok := message["message"].(map[string]any); ok {
		if stopReason, ok := extractStopReason(nested); ok {
			return stopReason, true
		}
	}
	return "", false
}

func decodeState(response rpcResponse) (rpcGetStateData, error) {
	data := rpcGetStateData{}
	if response.Data == nil {
		return data, fmt.Errorf("rpc get_state returned no data")
	}
	marshal, err := json.Marshal(response.Data)
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(marshal, &data); err != nil {
		return data, err
	}
	return data, nil
}

func decodeNewSession(response rpcResponse) (rpcNewSessionData, error) {
	data := rpcNewSessionData{}
	if response.Data == nil {
		return data, nil
	}
	marshal, err := json.Marshal(response.Data)
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(marshal, &data); err != nil {
		return data, err
	}
	return data, nil
}

func discoverWorkspaceSkills(workDir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(workDir, ".agents", "skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		path := filepath.Join(workDir, ".agents", "skills", entry.Name())
		out = append(out, path)
	}
	return out, nil
}
