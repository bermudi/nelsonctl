package pipeline

import "encoding/json"

// AgentInvokeEvent is emitted before an agent step begins.
type AgentInvokeEvent struct {
	Agent     string
	Step      string
	Model     string
	SessionID string
	Prompt    string
	WorkDir   string
}

// AgentResultEvent is emitted after an agent step finishes.
type AgentResultEvent struct {
	ExitCode   int
	DurationMs int64
	StdoutLen  int
	StderrLen  int
}

// ReviewResultEvent is emitted after a review step completes.
type ReviewResultEvent struct {
	Passed  bool
	Output  string
	Attempt int
	Step    string
	Phase   int
}

// GitCommitEvent is emitted after a successful git commit.
type GitCommitEvent struct {
	Subject string
	Files   []string
}

// GitPushEvent is emitted after a successful git push.
type GitPushEvent struct {
	Remote string
	Branch string
}

// PREvent is emitted after a pull request is created.
type PREvent struct {
	Title string
	URL   string
}

// ControllerToolCallStartEvent is emitted before dispatching a controller tool call.
type ControllerToolCallStartEvent struct {
	ID        string
	Tool      string
	Arguments json.RawMessage
}

// ControllerToolCallResultEvent is emitted after a controller tool call finishes.
type ControllerToolCallResultEvent struct {
	ID             string
	Tool           string
	Approved       bool
	Summary        string
	ContentLen     int
	UserMessageLen int
	Error          string
}

func (AgentInvokeEvent) pipelineEvent()              {}
func (AgentResultEvent) pipelineEvent()              {}
func (ReviewResultEvent) pipelineEvent()             {}
func (GitCommitEvent) pipelineEvent()                {}
func (GitPushEvent) pipelineEvent()                  {}
func (PREvent) pipelineEvent()                       {}
func (ControllerToolCallStartEvent) pipelineEvent()  {}
func (ControllerToolCallResultEvent) pipelineEvent() {}
