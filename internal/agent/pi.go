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

type piRPCAgent struct {
	settings        settings
	lookupPath      func(string) (string, error)
	client          rpcTransport
	events          chan Event
	implSessionID   string
	implSessionPath string
	mu              sync.Mutex
	closed          bool
	workspaceSkills []string
	lastSessionByID map[string]string
	activeSessionID string
	newClient       func(workDir string, skills []string) rpcTransport
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
		settings:        settings,
		lookupPath:      nil,
		events:          make(chan Event, settings.eventBufferSize()),
		lastSessionByID: map[string]string{},
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

func (a *piRPCAgent) ExecuteStep(ctx context.Context, step Step, prompt, model string) (*Result, error) {
	if rpc := a.AsRPC(); rpc != nil {
		sessionID, err := a.sessionForStep(ctx, step)
		if err != nil {
			return nil, err
		}
		return rpc.SendMessage(ctx, sessionID, prompt, pickModel(model, a.settings.modelFor(step)))
	}
	return nil, fmt.Errorf("pi rpc agent unavailable")
}

func (a *piRPCAgent) Cleanup(ctx context.Context) error {
	_ = ctx
	return a.Close()
}

func (a *piRPCAgent) AsRPC() RPCAgent {
	return a
}

func (a *piRPCAgent) Events() <-chan Event {
	return a.events
}

func (a *piRPCAgent) StartImplementationSession(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.implSessionID != "" {
		id := a.implSessionID
		a.mu.Unlock()
		return id, nil
	}
	a.mu.Unlock()
	if err := a.ensureClient(ctx); err != nil {
		return "", err
	}
	response, err := a.client.Send(ctx, rpcCommand{Type: "get_state"})
	if err != nil {
		return "", a.restartAfterCrash(ctx, err)
	}
	state, err := decodeState(response)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.implSessionID = state.SessionID
	a.implSessionPath = state.SessionFile
	a.activeSessionID = state.SessionID
	a.lastSessionByID[state.SessionID] = state.SessionFile
	return state.SessionID, nil
}

func (a *piRPCAgent) StartReviewSession(ctx context.Context) (string, error) {
	if err := a.ensureClient(ctx); err != nil {
		return "", err
	}
	parent := ""
	a.mu.Lock()
	parent = a.implSessionPath
	a.mu.Unlock()
	response, err := a.client.Send(ctx, rpcCommand{Type: "new_session", ParentSession: parent})
	if err != nil {
		return "", a.restartAfterCrash(ctx, err)
	}
	created, err := decodeNewSession(response)
	if err != nil {
		return "", err
	}
	if created.Cancelled {
		return "", fmt.Errorf("pi review session creation was cancelled")
	}
	stateResp, err := a.client.Send(ctx, rpcCommand{Type: "get_state"})
	if err != nil {
		return "", a.restartAfterCrash(ctx, err)
	}
	state, err := decodeState(stateResp)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.lastSessionByID[state.SessionID] = state.SessionFile
	implPath := a.implSessionPath
	a.mu.Unlock()
	if implPath != "" {
		_, _ = a.client.Send(ctx, rpcCommand{Type: "switch_session", SessionPath: implPath})
	}
	return state.SessionID, nil
}

func (a *piRPCAgent) SendMessage(ctx context.Context, sessionID, prompt, model string) (*Result, error) {
	if err := a.ensureClient(ctx); err != nil {
		return nil, err
	}
	if err := a.switchToSession(ctx, sessionID); err != nil {
		return nil, err
	}
	if model != "" {
		provider, modelID := splitModel(model)
		if provider != "" && modelID != "" {
			if _, err := a.client.Send(ctx, rpcCommand{Type: "set_model", Provider: provider, ModelID: modelID}); err != nil {
				return nil, a.restartAfterCrash(ctx, err)
			}
		}
	}

	start := time.Now()
	var stdout strings.Builder
	done := make(chan error, 1)
	unsubscribe := a.consumeSessionEvents(sessionID, &stdout, done)
	defer unsubscribe()

	if _, err := a.client.Send(ctx, rpcCommand{Type: "prompt", Message: prompt}); err != nil {
		return nil, a.restartAfterCrash(ctx, err)
	}
	stepCtx := ctx
	if timeout := a.settings.defaultTimeout; timeout > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	select {
	case err := <-done:
		res := &Result{Stdout: stdout.String(), ExitCode: 0, Duration: time.Since(start)}
		if err != nil {
			res.ExitCode = 1
			return res, err
		}
		a.emit(Event{Type: CompletionEvent, Content: "pi agent completed", Metadata: map[string]string{"session_id": sessionID}})
		return res, nil
	case <-stepCtx.Done():
		_ = a.Abort(context.Background(), sessionID)
		return &Result{Stdout: stdout.String(), ExitCode: 1, Duration: time.Since(start)}, stepCtx.Err()
	}
}

func (a *piRPCAgent) Abort(ctx context.Context, sessionID string) error {
	_ = sessionID
	if a.client == nil {
		return nil
	}
	_, err := a.client.Send(ctx, rpcCommand{Type: "abort"})
	return err
}

func (a *piRPCAgent) Close() error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	client := a.client
	a.client = nil
	a.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	close(a.events)
	return nil
}

func (a *piRPCAgent) ensureClient(ctx context.Context) error {
	a.mu.Lock()
	if a.client != nil {
		client := a.client
		a.mu.Unlock()
		_ = client
		return nil
	}
	a.mu.Unlock()
	skills, err := discoverWorkspaceSkills(a.settings.workDirectory())
	if err != nil {
		return err
	}
	client := a.newClient(a.settings.workDirectory(), skills)
	if err := client.Start(ctx); err != nil {
		return err
	}
	a.mu.Lock()
	a.client = client
	a.workspaceSkills = skills
	a.mu.Unlock()
	go a.forwardEvents(client)
	return nil
}

func (a *piRPCAgent) defaultNewClient(workDir string, skills []string) rpcTransport {
	client := newRPCClient(workDir, nil)
	client.starter = piProcessStarter(skills)
	return client
}

func piProcessStarter(skills []string) processStarter {
	return func(ctx context.Context, workDir string, env []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
		args := []string{"--mode", "rpc", "--no-extensions"}
		for _, skill := range skills {
			args = append(args, "--skill", skill)
		}
		return startPiRPCProcessWithArgs(ctx, workDir, env, args)
	}
}

func (a *piRPCAgent) sessionForStep(ctx context.Context, step Step) (string, error) {
	switch step {
	case StepApply, StepFix:
		return a.StartImplementationSession(ctx)
	case StepReview, StepFinalReview:
		return a.StartReviewSession(ctx)
	default:
		return a.StartImplementationSession(ctx)
	}
}

func (a *piRPCAgent) SessionForStep(ctx context.Context, step Step) (string, error) {
	return a.sessionForStep(ctx, step)
}

func (a *piRPCAgent) switchToSession(ctx context.Context, sessionID string) error {
	a.mu.Lock()
	sessionPath := a.lastSessionByID[sessionID]
	a.mu.Unlock()
	if sessionPath == "" {
		return fmt.Errorf("unknown pi session %s", sessionID)
	}
	_, err := a.client.Send(ctx, rpcCommand{Type: "switch_session", SessionPath: sessionPath})
	if err != nil {
		return a.restartAfterCrash(ctx, err)
	}
	a.mu.Lock()
	a.activeSessionID = sessionID
	a.mu.Unlock()
	return nil
}

func (a *piRPCAgent) consumeSessionEvents(sessionID string, stdout *strings.Builder, done chan<- error) func() {
	// Drain stale events from previous sessions before listening.
drain:
	for {
		select {
		case <-a.events:
		default:
			break drain
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-a.events:
				if !ok {
					done <- fmt.Errorf("pi process exited")
					return
				}
				if os.Getenv("NELSONCTL_DEBUG") != "" {
					fmt.Fprintf(os.Stderr, "[DEBUG-agent] consume event: type=%s session_id=%q target_session=%q content_len=%d\n", event.Type, event.Metadata["session_id"], sessionID, len(event.Content))
				}
				if event.Metadata["session_id"] != "" && event.Metadata["session_id"] != sessionID {
					continue
				}
				if event.Type == TextEvent {
					stdout.WriteString(event.Content)
				}
				if event.Type == CompletionEvent {
					done <- nil
					return
				}
				if event.Type == ErrorEvent {
					done <- errors.New(event.Content)
					return
				}
			}
		}
	}()
	return cancel
}

func (a *piRPCAgent) forwardEvents(client rpcTransport) {
	for event := range client.Events() {
		switch event.Type {
		case "extension_ui_request":
			if event.ID != "" {
				_ = client.SendNoResponse(rpcCommand{Type: "extension_ui_response", ID: event.ID, Cancelled: true})
			}
			continue
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
				case "thinking_delta", "thinking_start":
					// Ignore thinking tokens — not user-visible text
					content = ""
				default:
					// toolcall_*, text_end, thinking_end — no visible text
					content = ""
				}
			}
			if content != "" {
				a.emit(Event{Type: TextEvent, Content: content, Metadata: map[string]string{"session_id": event.SessionID}})
			}
		case "agent_end":
			sessionID := event.SessionID
			if sessionID == "" {
				// agent_end events from Pi RPC don't include sessionId.
				// Use the currently active session as fallback.
				a.mu.Lock()
				sessionID = a.activeSessionID
				a.mu.Unlock()
			}
			a.emit(Event{Type: CompletionEvent, Content: "agent_end", Metadata: map[string]string{"session_id": sessionID}})
		case "extension_error":
			a.emit(Event{Type: ErrorEvent, Content: event.Error})
		}
	}
}

func (a *piRPCAgent) restartAfterCrash(ctx context.Context, cause error) error {
	a.mu.Lock()
	client := a.client
	a.client = nil
	a.implSessionID = ""
	a.implSessionPath = ""
	a.lastSessionByID = map[string]string{}
	a.mu.Unlock()
	if client != nil {
		_ = client.Close()
	}
	if err := a.ensureClient(ctx); err != nil {
		return fmt.Errorf("restart pi after crash: %w", err)
	}
	a.emit(Event{Type: CompletionEvent, Content: "Nelson restarted Pi after a crash.", Metadata: map[string]string{"restart": "true"}})
	return fmt.Errorf("pi process exited unexpectedly; restarted and current phase must be re-run: %w", cause)
}

func (a *piRPCAgent) emit(event Event) {
	select {
	case a.events <- event:
	default:
	}
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
