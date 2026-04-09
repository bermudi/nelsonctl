package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bermudi/nelsonctl/internal/config"
)

type ToolName string

const (
	ToolReadFile     ToolName = "read_file"
	ToolGetDiff      ToolName = "get_diff"
	ToolSubmitPrompt ToolName = "submit_prompt"
	ToolRunReview    ToolName = "run_review"
	ToolApprove      ToolName = "approve"
)

type ReadFileArgs struct {
	Path string `json:"path"`
}

type GetDiffArgs struct{}

type SubmitPromptArgs struct {
	Prompt string `json:"prompt"`
}

type RunReviewArgs struct{}

type ApproveArgs struct {
	Summary string `json:"summary"`
}

type ToolDefinition struct {
	Name        ToolName
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	ID        string
	Name      ToolName
	Arguments json.RawMessage
}

type Message struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

func UserMessage(content string) Message {
	return Message{Role: "user", Content: strings.TrimSpace(content)}
}

type Phase struct {
	Number int
	Name   string
	Tasks  []string
}

type PhaseRequest struct {
	ChangeName    string
	Phase         Phase
	ReviewFailOn  config.ReviewFailOn
	InitialPrompt string
}

type FinalReviewRequest struct {
	ChangeName    string
	ReviewFailOn  config.ReviewFailOn
	InitialPrompt string
}

type Result struct {
	Summary   string
	ToolCalls int
	Messages  []Message
}

type Controller interface {
	RunPhase(ctx context.Context, request PhaseRequest, dispatcher Dispatcher) (*Result, error)
	RunFinalReview(ctx context.Context, request FinalReviewRequest, dispatcher Dispatcher) (*Result, error)
	Continue(ctx context.Context, messages []Message, dispatcher Dispatcher) (*Result, error)
}

type Option func(*openAIController)

func WithHTTPClient(client *http.Client) Option {
	return func(c *openAIController) {
		c.httpClient = client
	}
}

func WithEndpoint(endpoint string) Option {
	return func(c *openAIController) {
		c.endpoint = strings.TrimSpace(endpoint)
	}
}

func WithAPIKey(apiKey string) Option {
	return func(c *openAIController) {
		c.apiKey = strings.TrimSpace(apiKey)
	}
}

func WithSleep(sleep func(time.Duration)) Option {
	return func(c *openAIController) {
		if sleep != nil {
			c.sleep = sleep
		}
	}
}

func WithRetryPolicy(attempts int, baseBackoff time.Duration) Option {
	return func(c *openAIController) {
		if attempts > 0 {
			c.retryAttempts = attempts
		}
		if baseBackoff > 0 {
			c.baseBackoff = baseBackoff
		}
	}
}

type openAIController struct {
	controllerCfg config.ControllerConfig
	model         string
	endpoint      string
	apiKey        string
	httpClient    *http.Client
	sleep         func(time.Duration)
	retryAttempts int
	baseBackoff   time.Duration
}

func New(cfg config.Config, opts ...Option) (Controller, error) {
	providerInfo, ok := config.LookupControllerProvider(cfg.Controller.Provider)
	if !ok {
		return nil, fmt.Errorf("unsupported controller provider %q", cfg.Controller.Provider)
	}

	credential, _, err := config.ResolveControllerCredential(cfg.Controller.Provider, os.Getenv)
	if err != nil {
		return nil, err
	}

	if providerInfo.IsPoeResponses {
		return newPoeResponsesController(cfg, credential, opts...)
	}

	controller := &openAIController{
		controllerCfg: cfg.Controller,
		model:         cfg.Controller.Model,
		endpoint:      providerInfo.Endpoint,
		apiKey:        credential,
		httpClient:    &http.Client{},
		sleep:         time.Sleep,
		retryAttempts: 3,
		baseBackoff:   250 * time.Millisecond,
	}
	for _, opt := range opts {
		opt(controller)
	}

	if controller.model == "" {
		return nil, errors.New("controller model is required")
	}
	if controller.endpoint == "" {
		return nil, errors.New("controller endpoint is required")
	}
	if controller.apiKey == "" {
		return nil, fmt.Errorf("missing required controller credential %s", config.ControllerCredentialHint(cfg.Controller.Provider))
	}
	if controller.controllerCfg.MaxToolCalls <= 0 {
		return nil, errors.New("controller max tool calls must be greater than zero")
	}
	if controller.controllerCfg.Timeout.Std() <= 0 {
		return nil, errors.New("controller timeout must be greater than zero")
	}

	return controller, nil
}

func (c *openAIController) RunPhase(ctx context.Context, request PhaseRequest, dispatcher Dispatcher) (*Result, error) {
	initialPrompt := strings.TrimSpace(request.InitialPrompt)
	if initialPrompt == "" {
		initialPrompt = "Begin the phase controller loop. Read the relevant artifacts you need, then draft and send the apply prompt for this phase."
	}
	return c.run(ctx, PhaseSystemPrompt(request), initialPrompt, dispatcher)
}

func (c *openAIController) RunFinalReview(ctx context.Context, request FinalReviewRequest, dispatcher Dispatcher) (*Result, error) {
	initialPrompt := strings.TrimSpace(request.InitialPrompt)
	if initialPrompt == "" {
		initialPrompt = "Begin the final pre-archive controller loop. Use the available tools to run review, inspect relevant context, and approve only when the configured threshold is satisfied."
	}
	return c.run(ctx, FinalReviewSystemPrompt(request), initialPrompt, dispatcher)
}

func (c *openAIController) Continue(ctx context.Context, messages []Message, dispatcher Dispatcher) (*Result, error) {
	if len(messages) == 0 {
		return nil, errors.New("controller continuation requires existing messages")
	}
	return c.runConversation(ctx, cloneMessages(messages), dispatcher)
}

func (c *openAIController) run(ctx context.Context, systemPrompt, initialPrompt string, dispatcher Dispatcher) (*Result, error) {
	conversationCtx, cancel := context.WithTimeout(ctx, c.controllerCfg.Timeout.Std())
	defer cancel()

	return c.runConversation(conversationCtx, []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: initialPrompt},
	}, dispatcher)
}

func (c *openAIController) runConversation(ctx context.Context, messages []Message, dispatcher Dispatcher) (*Result, error) {
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

func (c *openAIController) complete(ctx context.Context, messages []Message) (Message, error) {
	request := openAIChatRequest{
		Model:      c.model,
		Messages:   toOpenAIMessages(messages),
		Tools:      toOpenAITools(ToolDefinitions()),
		ToolChoice: "auto",
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return Message{}, fmt.Errorf("marshal controller request: %w", err)
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

func (c *openAIController) completeOnce(ctx context.Context, payload []byte) (Message, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Message{}, fmt.Errorf("build controller request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return Message{}, apiError{Err: fmt.Errorf("controller API request failed: %w", err), Retryable: true}
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return Message{}, fmt.Errorf("read controller response: %w", err)
	}

	if response.StatusCode >= http.StatusInternalServerError || response.StatusCode == http.StatusTooManyRequests {
		return Message{}, apiError{Err: fmt.Errorf("controller API %s: %s", response.Status, strings.TrimSpace(string(body))), Retryable: true}
	}
	if response.StatusCode >= http.StatusBadRequest {
		return Message{}, apiError{Err: fmt.Errorf("controller API %s: %s", response.Status, strings.TrimSpace(string(body))), Retryable: false}
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Message{}, fmt.Errorf("decode controller response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Message{}, errors.New("controller response contained no choices")
	}

	return parsed.Choices[0].Message.toMessage(), nil
}

func ToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        ToolReadFile,
			Description: "Read a file from the workspace and return its contents.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "Workspace-relative file path to read."},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			},
		},
		{
			Name:        ToolGetDiff,
			Description: "Return the current git diff for uncommitted workspace changes.",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			Name:        ToolSubmitPrompt,
			Description: "Send a prompt to the implementation agent and wait for completion.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{"type": "string", "description": "Prompt to send to the implementation agent."},
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
		{
			Name:        ToolRunReview,
			Description: "Run the mechanical review flow and return the raw review output.",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
		{
			Name:        ToolApprove,
			Description: "Approve the phase or final review with a one-line summary.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"summary": map[string]any{"type": "string", "description": "Short approval summary."},
				},
				"required":             []string{"summary"},
				"additionalProperties": false,
			},
		},
	}
}

func cloneMessages(messages []Message) []Message {
	cloned := make([]Message, len(messages))
	for i, message := range messages {
		cloned[i] = Message{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  append([]ToolCall(nil), message.ToolCalls...),
		}
	}
	return cloned
}

type apiError struct {
	Err       error
	Retryable bool
}

func (e apiError) Error() string {
	if e.Err == nil {
		return "controller API error"
	}
	return e.Err.Error()
}

func (e apiError) Unwrap() error {
	return e.Err
}

type openAIChatRequest struct {
	Model      string          `json:"model"`
	Messages   []openAIMessage `json:"messages"`
	Tools      []openAITool    `json:"tools"`
	ToolChoice string          `json:"tool_choice,omitempty"`
}

type openAIChatResponse struct {
	Choices []openAIChoice `json:"choices"`
}

type openAIChoice struct {
	Message openAIMessagePayload `json:"message"`
}

type openAIMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCallBody `json:"tool_calls,omitempty"`
}

type openAIMessagePayload struct {
	Role      string               `json:"role"`
	Content   json.RawMessage      `json:"content,omitempty"`
	ToolCalls []openAIToolCallBody `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string                     `json:"type"`
	Function openAIToolFunctionMetadata `json:"function"`
}

type openAIToolFunctionMetadata struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCallBody struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openAIToolCallFunction `json:"function"`
}

type openAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func toOpenAIMessages(messages []Message) []openAIMessage {
	converted := make([]openAIMessage, len(messages))
	for i, message := range messages {
		converted[i] = openAIMessage{
			Role:       message.Role,
			Content:    message.Content,
			ToolCallID: message.ToolCallID,
			ToolCalls:  toOpenAIToolCalls(message.ToolCalls),
		}
	}
	return converted
}

func toOpenAIToolCalls(calls []ToolCall) []openAIToolCallBody {
	converted := make([]openAIToolCallBody, len(calls))
	for i, call := range calls {
		converted[i] = openAIToolCallBody{
			ID:   call.ID,
			Type: "function",
			Function: openAIToolCallFunction{
				Name:      string(call.Name),
				Arguments: string(call.Arguments),
			},
		}
	}
	return converted
}

func toOpenAITools(definitions []ToolDefinition) []openAITool {
	converted := make([]openAITool, len(definitions))
	for i, definition := range definitions {
		converted[i] = openAITool{
			Type: "function",
			Function: openAIToolFunctionMetadata{
				Name:        string(definition.Name),
				Description: definition.Description,
				Parameters:  definition.Parameters,
			},
		}
	}
	return converted
}

func (m openAIMessagePayload) toMessage() Message {
	message := Message{
		Role:    m.Role,
		Content: decodeContent(m.Content),
	}
	for _, call := range m.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, ToolCall{
			ID:        call.ID,
			Name:      ToolName(call.Function.Name),
			Arguments: json.RawMessage(call.Function.Arguments),
		})
	}
	return message
}

func decodeContent(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}

	var text string
	if err := json.Unmarshal(trimmed, &text); err == nil {
		return text
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteByte('\n')
			}
			builder.WriteString(part.Text)
		}
		return builder.String()
	}

	return string(trimmed)
}
