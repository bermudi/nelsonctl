package trace

import "time"

type RunMetaEvent struct {
	Type      string `json:"type"`
	Version   string `json:"version"`
	Agent     string `json:"agent"`
	Change    string `json:"change"`
	Hostname  string `json:"hostname"`
	StartedAt string `json:"started_at"`
}

type StateChangeEvent struct {
	Type         string `json:"type"`
	State        string `json:"state"`
	BranchReused bool   `json:"branch_reused,omitempty"`
	Ts           string `json:"ts"`
}

type PhaseStartEvent struct {
	Type    string `json:"type"`
	Phase   int    `json:"phase"`
	Name    string `json:"name"`
	Attempt int    `json:"attempt"`
	Ts      string `json:"ts"`
}

type AgentInvokeEvent struct {
	Type      string `json:"type"`
	Agent     string `json:"agent"`
	Step      string `json:"step"`
	Model     string `json:"model"`
	SessionID string `json:"session_id,omitempty"`
	Prompt    string `json:"prompt"`
	WorkDir   string `json:"work_dir"`
	Ts        string `json:"ts"`
}

type AgentResultEvent struct {
	Type       string `json:"type"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	StdoutLen  int    `json:"stdout_len"`
	StderrLen  int    `json:"stderr_len"`
	Ts         string `json:"ts"`
}

type OutputChunkEvent struct {
	Type  string `json:"type"`
	Chunk string `json:"chunk"`
	Ts    string `json:"ts"`
}

type SessionCreatedTraceEvent struct {
	Type          string `json:"type"`
	SessionID     string `json:"session_id"`
	SessionType   string `json:"session_type"`
	ParentSession string `json:"parent_session,omitempty"`
	Ts            string `json:"ts"`
}

type SessionSwitchedTraceEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Ts        string `json:"ts"`
}

type ModelSetTraceEvent struct {
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Success  bool   `json:"success"`
	Ts       string `json:"ts"`
}

type RPCRawTraceEvent struct {
	Type       string `json:"type"`
	RPCType    string `json:"rpc_type"`
	StopReason string `json:"stop_reason,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Ts         string `json:"ts"`
}

type EventsDrainedTraceEvent struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
	Ts    string `json:"ts"`
}

type AgentRestartedTraceEvent struct {
	Type  string `json:"type"`
	Cause string `json:"cause"`
	Ts    string `json:"ts"`
}

type ExecutionContextEvent struct {
	Type    string `json:"type"`
	Mode    string `json:"mode"`
	Agent   string `json:"agent"`
	Step    string `json:"step,omitempty"`
	Model   string `json:"model,omitempty"`
	Resumed bool   `json:"resumed,omitempty"`
	Ts      string `json:"ts"`
}

type ControllerActivityEvent struct {
	Type      string `json:"type"`
	Tool      string `json:"tool,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Analyzing bool   `json:"analyzing,omitempty"`
	Ts        string `json:"ts"`
}

type ReviewResultEvent struct {
	Type    string `json:"type"`
	Passed  bool   `json:"passed"`
	Output  string `json:"output"`
	Attempt int    `json:"attempt"`
	Step    string `json:"step"`
	Phase   int    `json:"phase,omitempty"`
	Ts      string `json:"ts"`
}

type PhaseResultEvent struct {
	Type     string `json:"type"`
	Phase    int    `json:"phase"`
	Passed   bool   `json:"passed"`
	Attempts int    `json:"attempts"`
	Review   string `json:"review"`
	Ts       string `json:"ts"`
}

type GitCommitEvent struct {
	Type    string   `json:"type"`
	Subject string   `json:"subject"`
	Files   []string `json:"files"`
	Ts      string   `json:"ts"`
}

type GitPushEvent struct {
	Type   string `json:"type"`
	Remote string `json:"remote"`
	Branch string `json:"branch"`
	Ts     string `json:"ts"`
}

type PREvent struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
	Ts    string `json:"ts"`
}

type TauntEvent struct {
	Type  string `json:"type"`
	Phase int    `json:"phase"`
	Ts    string `json:"ts"`
}

type SummaryEvent struct {
	Type            string `json:"type"`
	PhasesCompleted int    `json:"phases_completed"`
	PhasesFailed    int    `json:"phases_failed"`
	TotalAttempts   int    `json:"total_attempts,omitempty"`
	Duration        string `json:"duration"`
	Branch          string `json:"branch"`
	Mode            string `json:"mode,omitempty"`
	Resumed         bool   `json:"resumed,omitempty"`
	Ts              string `json:"ts"`
}

type RunEndEvent struct {
	Type            string `json:"type"`
	Status          string `json:"status"`
	DurationMs      int64  `json:"duration_ms"`
	PhasesCompleted int    `json:"phases_completed"`
	PhasesFailed    int    `json:"phases_failed"`
	Error           string `json:"error,omitempty"`
	Ts              string `json:"ts"`
}

func timestamp() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
