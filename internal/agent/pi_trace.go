package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

func summarizeRPCPayload(event rpcEvent) (json.RawMessage, error) {
	if len(event.Raw) == 0 {
		return nil, nil
	}
	var payload any
	if err := json.Unmarshal(event.Raw, &payload); err != nil {
		return nil, err
	}
	summary, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return summary, nil
}

func rpcPayloadSummary(event rpcEvent) string {
	parts := []string{event.Type}
	if event.AssistantUpdate != nil && strings.TrimSpace(event.AssistantUpdate.Type) != "" {
		parts = append(parts, event.AssistantUpdate.Type)
	}
	if event.ToolName != "" {
		parts = append(parts, "tool="+event.ToolName)
	}
	if event.ToolCallID != "" {
		parts = append(parts, "tool_call_id="+event.ToolCallID)
	}
	if event.SessionID != "" {
		parts = append(parts, "session="+event.SessionID)
	}
	return strings.Join(parts, " ")
}

func sessionIDForRPCEvent(event rpcEvent, fallback string) string {
	if strings.TrimSpace(event.SessionID) != "" {
		return strings.TrimSpace(event.SessionID)
	}
	return strings.TrimSpace(fallback)
}

func providerSummaryFromToolEvent(event rpcEvent) string {
	commandName, commandText, _ := commandDetailsFromToolEvent(event)
	parts := []string{event.ToolName}
	if commandName != "" {
		parts = append(parts, "command_name="+commandName)
	}
	if commandText != "" {
		parts = append(parts, fmt.Sprintf("command=%q", commandText))
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

func commandDetailsFromToolEvent(event rpcEvent) (string, string, []string) {
	if len(event.Args) == 0 {
		return event.ToolName, "", nil
	}
	commandText := strings.TrimSpace(asString(event.Args["command"]))
	commandName := event.ToolName
	commandArgs := []string{}
	if commandText != "" {
		fields := strings.Fields(commandText)
		if len(fields) > 0 {
			commandName = fields[0]
			if len(fields) > 1 {
				commandArgs = append(commandArgs, fields[1:]...)
			}
		}
	}
	return commandName, commandText, commandArgs
}

func toolResultSummary(event rpcEvent) (stdoutLen int, stderrLen int, exitCode int) {
	result := event.Result
	if len(result) == 0 {
		if event.IsError {
			return 0, len(errorTextFromToolEvent(event)), 1
		}
		return 0, 0, 0
	}
	stdoutLen = len(textFromToolContent(result["content"]))
	stderrLen = len(asString(result["stderr"]))
	exitCode = asInt(result["exitCode"])
	if exitCode == 0 && event.IsError {
		exitCode = 1
	}
	return stdoutLen, stderrLen, exitCode
}

func errorTextFromToolEvent(event rpcEvent) string {
	if !event.IsError {
		return ""
	}
	if text := asString(event.Result["error"]); text != "" {
		return text
	}
	return textFromToolContent(event.Result["content"])
}

func textFromToolContent(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text := asString(entry["text"])
		if text == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(text)
	}
	return builder.String()
}

func asString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

// asInt safely converts a value to int with bounds checking to prevent overflow.
// Values outside int range are clamped to math.MaxInt or math.MinInt.
func asInt(value any) int {
	switch v := value.(type) {
	case float64:
		if v > float64(math.MaxInt) {
			return math.MaxInt
		}
		if v < float64(math.MinInt) {
			return math.MinInt
		}
		return int(v)
	case float32:
		if v > float32(math.MaxInt) {
			return math.MaxInt
		}
		if v < float32(math.MinInt) {
			return math.MinInt
		}
		return int(v)
	case int:
		return v
	case int64:
		if v > int64(math.MaxInt) {
			return math.MaxInt
		}
		if v < int64(math.MinInt) {
			return math.MinInt
		}
		return int(v)
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			if parsed > int64(math.MaxInt) {
				return math.MaxInt
			}
			if parsed < int64(math.MinInt) {
				return math.MinInt
			}
			return int(parsed)
		}
	}
	return 0
}
