package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bermudi/nelsonctl/internal/config"
)

func TestControllerRunsPhaseLoopAndApproves(t *testing.T) {
	server := newMockControllerServer([]mockResponse{
		{ToolCalls: []mockToolCall{{ID: "1", Name: string(ToolReadFile), Arguments: `{"path":"specs/changes/pi-rpc-integration/proposal.md"}`}}},
		{ToolCalls: []mockToolCall{{ID: "2", Name: string(ToolSubmitPrompt), Arguments: `{"prompt":"implement it"}`}}},
		{ToolCalls: []mockToolCall{{ID: "3", Name: string(ToolRunReview), Arguments: `{}`}}},
		{ToolCalls: []mockToolCall{{ID: "4", Name: string(ToolApprove), Arguments: `{"summary":"phase passed"}`}}},
	})
	defer server.Close()

	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	cfg := config.DefaultConfig()
	controller, err := New(cfg, WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var dispatched []ToolName
	dispatcher := NewToolDispatcher(Handlers{
		RepoDir: t.TempDir(),
		ReadFile: func(ctx context.Context, path string) (string, error) {
			dispatched = append(dispatched, ToolReadFile)
			return "proposal", nil
		},
		SubmitPrompt: func(ctx context.Context, prompt string) (string, error) {
			dispatched = append(dispatched, ToolSubmitPrompt)
			if prompt != "implement it" {
				t.Fatalf("prompt = %q", prompt)
			}
			return "Agent completed successfully.", nil
		},
		RunReview: func(ctx context.Context) (string, error) {
			dispatched = append(dispatched, ToolRunReview)
			return "review output", nil
		},
		Approve: func(ctx context.Context, summary string) error {
			dispatched = append(dispatched, ToolApprove)
			if summary != "phase passed" {
				t.Fatalf("summary = %q", summary)
			}
			return nil
		},
	})

	result, err := controller.RunPhase(context.Background(), PhaseRequest{
		ChangeName:   "pi-rpc-integration",
		ReviewFailOn: config.FailOnCritical,
		Phase:        Phase{Number: 2, Name: "Controller AI", Tasks: []string{"Define the controller", "Implement the loop"}},
	}, dispatcher)
	if err != nil {
		t.Fatalf("RunPhase() error = %v", err)
	}
	if result.Summary != "phase passed" {
		t.Fatalf("Summary = %q", result.Summary)
	}
	if result.ToolCalls != 4 {
		t.Fatalf("ToolCalls = %d, want 4", result.ToolCalls)
	}
	if got, want := fmt.Sprint(dispatched), fmt.Sprint([]ToolName{ToolReadFile, ToolSubmitPrompt, ToolRunReview, ToolApprove}); got != want {
		t.Fatalf("dispatched = %s, want %s", got, want)
	}
}

func TestControllerEnforcesToolBudget(t *testing.T) {
	server := newMockControllerServer([]mockResponse{{ToolCalls: []mockToolCall{{ID: "1", Name: string(ToolGetDiff), Arguments: `{}`}, {ID: "2", Name: string(ToolRunReview), Arguments: `{}`}, {ID: "3", Name: string(ToolApprove), Arguments: `{"summary":"done"}`}}}})
	defer server.Close()

	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	cfg := config.DefaultConfig()
	cfg.Controller.MaxToolCalls = 2
	controller, err := New(cfg, WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = controller.RunPhase(context.Background(), PhaseRequest{ChangeName: "pi-rpc-integration", ReviewFailOn: config.FailOnCritical, Phase: Phase{Number: 2, Name: "Controller AI"}}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err == nil || !strings.Contains(err.Error(), "tool call budget") {
		t.Fatalf("expected tool budget error, got %v", err)
	}
}

func TestControllerEnforcesConversationTimeout(t *testing.T) {
	server := newMockControllerServer([]mockResponse{{Delay: 50 * time.Millisecond, ToolCalls: []mockToolCall{{ID: "1", Name: string(ToolApprove), Arguments: `{"summary":"done"}`}}}})
	defer server.Close()

	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	cfg := config.DefaultConfig()
	cfg.Controller.Timeout = config.Duration(10 * time.Millisecond)
	controller, err := New(cfg, WithHTTPClient(server.Client()), WithEndpoint(server.URL))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = controller.RunPhase(context.Background(), PhaseRequest{ChangeName: "pi-rpc-integration", ReviewFailOn: config.FailOnCritical, Phase: Phase{Number: 2, Name: "Controller AI"}}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestControllerRetriesAPIFailuresWithBackoff(t *testing.T) {
	server := newFlakyControllerServer(2, mockResponse{ToolCalls: []mockToolCall{{ID: "1", Name: string(ToolApprove), Arguments: `{"summary":"done"}`}}})
	defer server.Close()

	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	cfg := config.DefaultConfig()
	var sleeps []time.Duration
	controller, err := New(cfg,
		WithHTTPClient(server.Client()),
		WithEndpoint(server.URL),
		WithRetryPolicy(3, 10*time.Millisecond),
		WithSleep(func(d time.Duration) { sleeps = append(sleeps, d) }),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := controller.RunPhase(context.Background(), PhaseRequest{ChangeName: "pi-rpc-integration", ReviewFailOn: config.FailOnCritical, Phase: Phase{Number: 2, Name: "Controller AI"}}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err != nil {
		t.Fatalf("RunPhase() error = %v", err)
	}
	if result.Summary != "done" {
		t.Fatalf("Summary = %q", result.Summary)
	}
	if got, want := len(sleeps), 2; got != want {
		t.Fatalf("len(sleeps) = %d, want %d", got, want)
	}
	if sleeps[0] != 10*time.Millisecond || sleeps[1] != 20*time.Millisecond {
		t.Fatalf("sleeps = %#v", sleeps)
	}
}

func TestControllerFailsAfterPersistentAPIFailures(t *testing.T) {
	server := newAlwaysFailingControllerServer()
	defer server.Close()

	t.Setenv("DEEPSEEK_API_KEY", "test-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	cfg := config.DefaultConfig()
	var sleeps []time.Duration
	controller, err := New(cfg,
		WithHTTPClient(server.Client()),
		WithEndpoint(server.URL),
		WithRetryPolicy(3, 5*time.Millisecond),
		WithSleep(func(d time.Duration) { sleeps = append(sleeps, d) }),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = controller.RunPhase(context.Background(), PhaseRequest{ChangeName: "pi-rpc-integration", ReviewFailOn: config.FailOnCritical, Phase: Phase{Number: 2, Name: "Controller AI"}}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err == nil || !strings.Contains(err.Error(), "controller API") {
		t.Fatalf("expected controller API error, got %v", err)
	}
	if got, want := len(sleeps), 2; got != want {
		t.Fatalf("len(sleeps) = %d, want %d", got, want)
	}
}

func TestNewUsesProviderEndpoints(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-key")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	t.Setenv("POE_API_KEY", "poe-key")
	t.Setenv("POE_OAUTH_TOKEN", "poe-oauth-token")

	cfg := config.DefaultConfig()
	controller, err := New(cfg)
	if err != nil {
		t.Fatalf("New() opencode error = %v", err)
	}
	oc := controller.(*openAIController)
	if oc.endpoint != "https://openrouter.ai/api/v1/chat/completions" {
		t.Fatalf("opencode endpoint = %q", oc.endpoint)
	}

	cfg.Controller.Provider = config.ProviderDeepSeek
	controller, err = New(cfg)
	if err != nil {
		t.Fatalf("New() deepseek error = %v", err)
	}
	deepseek := controller.(*openAIController)
	if deepseek.endpoint != "https://api.deepseek.com/chat/completions" {
		t.Fatalf("deepseek endpoint = %q", deepseek.endpoint)
	}

	cfg.Controller.Provider = config.ProviderOpenRouter
	controller, err = New(cfg)
	if err != nil {
		t.Fatalf("New() openrouter error = %v", err)
	}
	openrouter := controller.(*openAIController)
	if openrouter.endpoint != "https://openrouter.ai/api/v1/chat/completions" {
		t.Fatalf("openrouter endpoint = %q", openrouter.endpoint)
	}

	cfg.Controller.Provider = config.ProviderPoe
	controller, err = New(cfg)
	if err != nil {
		t.Fatalf("New() poe error = %v", err)
	}
	poe := controller.(*openAIController)
	if poe.endpoint != "https://api.poe.com/v1/chat/completions" {
		t.Fatalf("poe endpoint = %q", poe.endpoint)
	}
	if poe.apiKey != "poe-key" {
		t.Fatalf("poe credential = %q", poe.apiKey)
	}

	_ = os.Unsetenv("POE_API_KEY")
	controller, err = New(cfg)
	if err != nil {
		t.Fatalf("New() poe oauth error = %v", err)
	}
	poe = controller.(*openAIController)
	if poe.apiKey != "poe-oauth-token" {
		t.Fatalf("poe oauth credential = %q", poe.apiKey)
	}
}

type mockResponse struct {
	Delay     time.Duration
	Content   string
	ToolCalls []mockToolCall
}

type mockToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func newMockControllerServer(responses []mockResponse) *httptest.Server {
	var mu sync.Mutex
	index := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if index >= len(responses) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"unexpected extra request"}`)
			return
		}
		response := responses[index]
		index++
		if response.Delay > 0 {
			time.Sleep(response.Delay)
		}
		writeMockResponse(w, response)
	}))
}

func newFlakyControllerServer(failures int, success mockResponse) *httptest.Server {
	var mu sync.Mutex
	attempt := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		attempt++
		if attempt <= failures {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"error":"temporary upstream failure"}`)
			return
		}
		writeMockResponse(w, success)
	}))
}

func newAlwaysFailingControllerServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":"persistent upstream failure"}`)
	}))
}

func writeMockResponse(w http.ResponseWriter, response mockResponse) {
	choice := map[string]any{"message": map[string]any{"role": "assistant"}}
	if response.Content != "" {
		choice["message"].(map[string]any)["content"] = response.Content
	}
	if len(response.ToolCalls) > 0 {
		toolCalls := make([]map[string]any, 0, len(response.ToolCalls))
		for _, call := range response.ToolCalls {
			toolCalls = append(toolCalls, map[string]any{
				"id":   call.ID,
				"type": "function",
				"function": map[string]any{
					"name":      call.Name,
					"arguments": call.Arguments,
				},
			})
		}
		choice["message"].(map[string]any)["tool_calls"] = toolCalls
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{choice}})
}
