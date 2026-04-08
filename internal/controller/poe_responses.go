package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bermudi/nelsonctl/internal/config"
)

type poeResponsesController struct {
	controllerCfg config.ControllerConfig
	model         string
	endpoint      string
	apiKey        string
	httpClient    *http.Client
	sleep         func(time.Duration)
	retryAttempts int
	baseBackoff   time.Duration
}

func newPoeResponsesController(cfg config.Config, credential string, opts ...Option) (*poeResponsesController, error) {
	c := &poeResponsesController{
		controllerCfg: cfg.Controller,
		model:         cfg.Controller.Model,
		endpoint:      "https://api.poe.com/bot/" + cfg.Controller.Model,
		apiKey:        credential,
		httpClient:    &http.Client{},
		sleep:         time.Sleep,
		retryAttempts: 3,
		baseBackoff:   250 * time.Millisecond,
	}
	for _, opt := range opts {
		// Apply shared options by creating a temporary openAIController
		tmp := &openAIController{httpClient: c.httpClient, sleep: c.sleep, retryAttempts: c.retryAttempts, baseBackoff: c.baseBackoff}
		opt(tmp)
		c.httpClient = tmp.httpClient
		c.sleep = tmp.sleep
		c.retryAttempts = tmp.retryAttempts
		c.baseBackoff = tmp.baseBackoff
	}

	if c.model == "" {
		return nil, errors.New("controller model is required")
	}
	if c.apiKey == "" {
		return nil, fmt.Errorf("missing required controller credential %s", config.ControllerCredentialHint(cfg.Controller.Provider))
	}
	if c.controllerCfg.MaxToolCalls <= 0 {
		return nil, errors.New("controller max tool calls must be greater than zero")
	}
	if c.controllerCfg.Timeout.Std() <= 0 {
		return nil, errors.New("controller timeout must be greater than zero")
	}
	return c, nil
}

func (c *poeResponsesController) RunPhase(ctx context.Context, request PhaseRequest, dispatcher Dispatcher) (*Result, error) {
	initialPrompt := strings.TrimSpace(request.InitialPrompt)
	if initialPrompt == "" {
		initialPrompt = "Begin the phase controller loop. Read the relevant artifacts you need, then draft and send the apply prompt for this phase."
	}
	return c.run(ctx, PhaseSystemPrompt(request), initialPrompt, dispatcher)
}

func (c *poeResponsesController) RunFinalReview(ctx context.Context, request FinalReviewRequest, dispatcher Dispatcher) (*Result, error) {
	initialPrompt := strings.TrimSpace(request.InitialPrompt)
	if initialPrompt == "" {
		initialPrompt = "Begin the final pre-archive controller loop. Use the available tools to run review, inspect relevant context, and approve only when the configured threshold is satisfied."
	}
	return c.run(ctx, FinalReviewSystemPrompt(request), initialPrompt, dispatcher)
}

func (c *poeResponsesController) Continue(ctx context.Context, messages []Message, dispatcher Dispatcher) (*Result, error) {
	if len(messages) == 0 {
		return nil, errors.New("controller continuation requires existing messages")
	}
	return c.runConversation(ctx, cloneMessages(messages), dispatcher)
}

func (c *poeResponsesController) run(ctx context.Context, systemPrompt, initialPrompt string, dispatcher Dispatcher) (*Result, error) {
	conversationCtx, cancel := context.WithTimeout(ctx, c.controllerCfg.Timeout.Std())
	defer cancel()
	return c.runConversation(conversationCtx, []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: initialPrompt},
	}, dispatcher)
}

func (c *poeResponsesController) runConversation(ctx context.Context, messages []Message, dispatcher Dispatcher) (*Result, error) {
	toolCallsUsed := 0
	for {
		assistant, err := c.complete(ctx, messages)
		if err != nil {
			return nil, err
		}
		messages = append(messages, assistant)

		if len(assistant.ToolCalls) == 0 {
			content := strings.TrimSpace(assistant.Content)
			if content == "" {
				return nil, errors.New("controller returned no tool calls and no content")
			}
			return nil, fmt.Errorf("controller returned no tool calls before approval: %s", content)
		}

		if toolCallsUsed+len(assistant.ToolCalls) > c.controllerCfg.MaxToolCalls {
			return nil, fmt.Errorf("controller exceeded tool call budget of %d", c.controllerCfg.MaxToolCalls)
		}

		for _, call := range assistant.ToolCalls {
			toolCallsUsed++
			result, err := dispatcher.Dispatch(ctx, call)
			if err != nil {
				return nil, err
			}
			toolMessage := Message{Role: "tool", ToolCallID: call.ID, Content: result.Content}
			messages = append(messages, toolMessage)
			if strings.TrimSpace(result.UserMessage) != "" {
				messages = append(messages, UserMessage(result.UserMessage))
			}
			if result.Approved {
				return &Result{
					Summary:   result.Summary,
					ToolCalls: toolCallsUsed,
					Messages:  cloneMessages(messages),
				}, nil
			}
		}
	}
}

func (c *poeResponsesController) complete(ctx context.Context, messages []Message) (Message, error) {
	req := c.buildRequest(messages)
	payload, err := json.Marshal(req)
	if err != nil {
		return Message{}, fmt.Errorf("marshal poe responses request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= c.retryAttempts; attempt++ {
		message, err := c.completeOnce(ctx, payload)
		if err == nil {
			return message, nil
		}
		lastErr = err
		var retryableErr apiError
		if !errors.As(err, &retryableErr) || !retryableErr.Retryable || attempt == c.retryAttempts {
			return Message{}, lastErr
		}
		backoff := c.baseBackoff * time.Duration(1<<(attempt-1))
		if c.sleep != nil {
			c.sleep(backoff)
		}
	}
	return Message{}, lastErr
}

func (c *poeResponsesController) buildRequest(messages []Message) poeRequest {
	req := poeRequest{
		Tools: toPoeResponsesTools(ToolDefinitions()),
	}

	// Extract system instruction from system messages
	var conversationMsgs []Message
	for _, msg := range messages {
		if msg.Role == "system" {
			req.SystemInstruction = msg.Content
			continue
		}
		conversationMsgs = append(conversationMsgs, msg)
	}

	// If only one user message and no tool-related messages, use query field
	if len(conversationMsgs) == 1 && conversationMsgs[0].Role == "user" && len(conversationMsgs[0].ToolCalls) == 0 {
		req.Query = conversationMsgs[0].Content
		return req
	}

	// Multi-turn: build messages array and extract pending tool results
	var poeMsgs []poeMessage
	var pendingToolCalls []poeToolCall
	var toolResults []poeToolResult

	for _, msg := range conversationMsgs {
		switch msg.Role {
		case "user":
			poeMsgs = append(poeMsgs, poeMessage{Role: "user", Content: msg.Content})
		case "assistant":
			pm := poeMessage{Role: "assistant", Content: msg.Content}
			for _, tc := range msg.ToolCalls {
				pm.ToolCalls = append(pm.ToolCalls, poeToolCall{
					ID:        tc.ID,
					Name:      string(tc.Name),
					Arguments: json.RawMessage(tc.Arguments),
				})
			}
			poeMsgs = append(poeMsgs, pm)
			// Track the latest assistant tool calls for the tool_calls field
			if len(msg.ToolCalls) > 0 {
				pendingToolCalls = pm.ToolCalls
			}
		case "tool":
			toolResults = append(toolResults, poeToolResult{
				ToolCallID: msg.ToolCallID,
				Output:     msg.Content,
			})
		}
	}

	req.Messages = poeMsgs
	req.ToolCalls = pendingToolCalls
	req.ToolResults = toolResults
	return req
}

func (c *poeResponsesController) completeOnce(ctx context.Context, payload []byte) (Message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Message{}, fmt.Errorf("build poe responses request: %w", err)
	}
	req.Header.Set("Poe-API-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Message{}, apiError{Err: fmt.Errorf("poe responses API request failed: %w", err), Retryable: true}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("read poe responses response: %w", err)
	}

	if resp.StatusCode >= http.StatusInternalServerError || resp.StatusCode == http.StatusTooManyRequests {
		return Message{}, apiError{Err: fmt.Errorf("poe responses API %s: %s", resp.Status, strings.TrimSpace(string(body))), Retryable: true}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return Message{}, apiError{Err: fmt.Errorf("poe responses API %s: %s", resp.Status, strings.TrimSpace(string(body))), Retryable: false}
	}

	var parsed poeResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode poe responses response: %w", err)
	}

	message := Message{
		Role:    "assistant",
		Content: parsed.Text,
	}
	for _, tc := range parsed.ToolCalls {
		// Poe Responses API returns arguments as a JSON object, convert to raw message string
		argsBytes, _ := json.Marshal(tc.Arguments)
		message.ToolCalls = append(message.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      ToolName(tc.Name),
			Arguments: json.RawMessage(argsBytes),
		})
	}
	return message, nil
}

// Poe Responses API types

type poeRequest struct {
	Query            string           `json:"query,omitempty"`
	SystemInstruction string          `json:"system_instruction,omitempty"`
	Messages         []poeMessage     `json:"messages,omitempty"`
	Tools            []poeToolDef     `json:"tools,omitempty"`
	ToolCalls        []poeToolCall    `json:"tool_calls,omitempty"`
	ToolResults      []poeToolResult  `json:"tool_results,omitempty"`
}

type poeMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content"`
	ToolCalls []poeToolCall  `json:"tool_calls,omitempty"`
}

type poeToolDef struct {
	Type     string           `json:"type"`
	Function poeToolFunction  `json:"function"`
}

type poeToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type poeToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type poeToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Output     string `json:"output"`
}

type poeResponse struct {
	Text      string        `json:"text"`
	ToolCalls []poeToolCall `json:"tool_calls,omitempty"`
}

func toPoeResponsesTools(defs []ToolDefinition) []poeToolDef {
	converted := make([]poeToolDef, len(defs))
	for i, def := range defs {
		converted[i] = poeToolDef{
			Type: "function",
			Function: poeToolFunction{
				Name:        string(def.Name),
				Description: def.Description,
				Parameters:  def.Parameters,
			},
		}
	}
	return converted
}
