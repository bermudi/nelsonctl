package agent

type rpcCommand struct {
	ID            string                 `json:"id,omitempty"`
	Type          string                 `json:"type"`
	Message       string                 `json:"message,omitempty"`
	ParentSession string                 `json:"parentSession,omitempty"`
	SessionPath   string                 `json:"sessionPath,omitempty"`
	Provider      string                 `json:"provider,omitempty"`
	ModelID       string                 `json:"modelId,omitempty"`
	Cancelled     bool                   `json:"cancelled,omitempty"`
	Confirmed     bool                   `json:"confirmed,omitempty"`
	Value         string                 `json:"value,omitempty"`
	Payload       map[string]interface{} `json:"-"`
}

type rpcResponse struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Command string         `json:"command"`
	Success bool           `json:"success"`
	Data    map[string]any `json:"data,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type rpcEventEnvelope struct {
	Type string `json:"type"`
}

type rpcGetStateData struct {
	SessionFile string `json:"sessionFile"`
	SessionID   string `json:"sessionId"`
	IsStreaming bool   `json:"isStreaming"`
}

type rpcNewSessionData struct {
	Cancelled bool `json:"cancelled"`
}

type rpcEvent struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	Method          string                    `json:"method,omitempty"`
	Message         string                    `json:"message,omitempty"`
	NotifyType      string                    `json:"notifyType,omitempty"`
	StatusKey       string                    `json:"statusKey,omitempty"`
	StatusText      string                    `json:"statusText,omitempty"`
	Title           string                    `json:"title,omitempty"`
	Text            string                    `json:"text,omitempty"`
	WidgetKey       string                    `json:"widgetKey,omitempty"`
	SessionFile     string                    `json:"sessionFile,omitempty"`
	SessionID       string                    `json:"sessionId,omitempty"`
	ExtensionPath   string                    `json:"extensionPath,omitempty"`
	Error           string                    `json:"error,omitempty"`
	Event           string                    `json:"event,omitempty"`
	AssistantUpdate *rpcAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`
	MessageData     map[string]any            `json:"message,omitempty"`
	Messages        []map[string]any          `json:"messages,omitempty"`
}

type rpcAssistantMessageEvent struct {
	Type string `json:"type,omitempty"`
	Part struct {
		Text string `json:"text,omitempty"`
	} `json:"part,omitempty"`
}

type rpcExtensionUIResponse struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Cancelled bool   `json:"cancelled,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
	Value     string `json:"value,omitempty"`
}
