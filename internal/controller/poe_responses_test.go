package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bermudi/nelsonctl/internal/config"
)

func TestPoeResponsesRunsPhaseLoopAndApproves(t *testing.T) {
	server := newPoeResponsesMockServer([]poeMockResponse{
		{ToolCalls: []poeMockToolCall{{ID: "1", Name: "read_file", Arguments: json.RawMessage(`{"path":"specs/proposal.md"}`)}}},
		{ToolCalls: []poeMockToolCall{{ID: "2", Name: "submit_prompt", Arguments: json.RawMessage(`{"prompt":"implement it"}`)}}},
		{ToolCalls: []poeMockToolCall{{ID: "3", Name: "run_review", Arguments: json.RawMessage(`{}`)}}},
		{ToolCalls: []poeMockToolCall{{ID: "4", Name: "approve", Arguments: json.RawMessage(`{"summary":"phase passed"}`)}}},
	})
	defer server.Close()

	t.Setenv("POE_API_KEY", "test-key")
	cfg := config.DefaultConfig()
	cfg.Controller.Provider = config.ProviderPoeResponses
	cfg.Controller.Model = "claude-3-5-sonnet"

	ctrl, err := newPoeResponsesController(cfg, "test-key",
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("newPoeResponsesController() error = %v", err)
	}
	ctrl.endpoint = server.URL

	result, err := ctrl.RunPhase(context.Background(), PhaseRequest{
		ChangeName:   "test-change",
		ReviewFailOn: config.FailOnCritical,
		Phase:        Phase{Number: 1, Name: "Test Phase", Tasks: []string{"Do the thing"}},
	}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err != nil {
		t.Fatalf("RunPhase() error = %v", err)
	}
	if result.Summary != "phase passed" {
		t.Fatalf("summary = %q", result.Summary)
	}
	if result.ToolCalls != 4 {
		t.Fatalf("toolCalls = %d, want 4", result.ToolCalls)
	}
}

func TestPoeResponsesSendsCorrectFormat(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Poe-API-Key") != "test-key" {
			t.Errorf("Poe-API-Key = %q, want test-key", r.Header.Get("Poe-API-Key"))
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("should not send Authorization header for Poe Responses API")
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"text":"done","tool_calls":[{"id":"1","name":"approve","arguments":{"summary":"ok"}}]}`)
	}))
	defer server.Close()

	t.Setenv("POE_API_KEY", "test-key")
	cfg := config.DefaultConfig()
	cfg.Controller.Provider = config.ProviderPoeResponses
	cfg.Controller.Model = "claude-3-5-sonnet"

	ctrl, err := newPoeResponsesController(cfg, "test-key",
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("newPoeResponsesController() error = %v", err)
	}
	ctrl.endpoint = server.URL

	_, err = ctrl.RunPhase(context.Background(), PhaseRequest{
		ChangeName:   "test",
		ReviewFailOn: config.FailOnCritical,
		Phase:        Phase{Number: 1, Name: "Test"},
	}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err != nil {
		t.Fatalf("RunPhase() error = %v", err)
	}

	// Verify first request uses query field (single user message)
	if _, ok := gotBody["query"]; !ok {
		t.Fatalf("first request should have query field, got keys: %v", mapKeys(gotBody))
	}
	if sys, ok := gotBody["system_instruction"].(string); !ok || !strings.Contains(sys, "phase 1") {
		t.Fatalf("system_instruction missing phase context")
	}
	if tools, ok := gotBody["tools"].([]any); !ok || len(tools) != 5 {
		t.Fatalf("tools count = %d, want 5", len(tools))
	}
}

func TestPoeResponsesUsesMessagesForMultiTurn(t *testing.T) {
	callCount := 0
	var secondBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		body, _ := io.ReadAll(r.Body)

		if callCount == 1 {
			// First call: return a tool call
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"text":"","tool_calls":[{"id":"tc1","name":"read_file","arguments":{"path":"test.go"}}]}`)
		} else {
			// Second call: capture the body for inspection
			json.Unmarshal(body, &secondBody)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"text":"","tool_calls":[{"id":"tc2","name":"approve","arguments":{"summary":"done"}}]}`)
		}
	}))
	defer server.Close()

	t.Setenv("POE_API_KEY", "test-key")
	cfg := config.DefaultConfig()
	cfg.Controller.Provider = config.ProviderPoeResponses
	cfg.Controller.Model = "claude-3-5-sonnet"

	ctrl, err := newPoeResponsesController(cfg, "test-key",
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("newPoeResponsesController() error = %v", err)
	}
	ctrl.endpoint = server.URL

	_, err = ctrl.RunPhase(context.Background(), PhaseRequest{
		ChangeName:   "test",
		ReviewFailOn: config.FailOnCritical,
		Phase:        Phase{Number: 1, Name: "Test"},
	}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err != nil {
		t.Fatalf("RunPhase() error = %v", err)
	}

	// Second request should use messages array (multi-turn)
	if _, ok := secondBody["messages"]; !ok {
		t.Fatalf("second request should have messages field, got keys: %v", mapKeys(secondBody))
	}
	if _, ok := secondBody["tool_calls"]; !ok {
		t.Fatalf("second request should have tool_calls field, got keys: %v", mapKeys(secondBody))
	}
	if _, ok := secondBody["tool_results"]; !ok {
		t.Fatalf("second request should have tool_results field, got keys: %v", mapKeys(secondBody))
	}
}

// Poe Responses mock helpers

type poeMockResponse struct {
	Text      string
	ToolCalls []poeMockToolCall
}

type poeMockToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func newPoeResponsesMockServer(responses []poeMockResponse) *httptest.Server {
	var callIndex int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if callIndex >= len(responses) {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"unexpected extra request"}`)
			return
		}
		resp := responses[callIndex]
		callIndex++

		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{
			"text": resp.Text,
		}
		if len(resp.ToolCalls) > 0 {
			result["tool_calls"] = resp.ToolCalls
		}
		bytes, _ := json.Marshal(result)
		w.Write(bytes)
	}))
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestPoeResponsesRetriesOnTransientFailures(t *testing.T) {
	t.Setenv("POE_API_KEY", "test-key")
	cfg := config.DefaultConfig()
	cfg.Controller.Provider = config.ProviderPoeResponses
	cfg.Controller.Model = "claude-3-5-sonnet"

	var sleeps []time.Duration
	failCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCount++
		if failCount <= 2 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"text":"","tool_calls":[{"id":"1","name":"approve","arguments":{"summary":"ok"}}]}`)
	}))
	defer server.Close()

	ctrl, err := newPoeResponsesController(cfg, "test-key",
		WithHTTPClient(server.Client()),
		WithSleep(func(d time.Duration) { sleeps = append(sleeps, d) }),
	)
	if err != nil {
		t.Fatalf("newPoeResponsesController() error = %v", err)
	}
	ctrl.endpoint = server.URL

	_, err = ctrl.RunPhase(context.Background(), PhaseRequest{
		ChangeName:   "test",
		ReviewFailOn: config.FailOnCritical,
		Phase:        Phase{Number: 1, Name: "Test"},
	}, NewToolDispatcher(Handlers{RepoDir: t.TempDir()}))
	if err != nil {
		t.Fatalf("RunPhase() error = %v", err)
	}
	if got, want := len(sleeps), 2; got != want {
		t.Fatalf("sleeps = %d, want %d", got, want)
	}
}
