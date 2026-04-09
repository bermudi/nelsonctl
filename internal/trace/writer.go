package trace

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/bermudi/nelsonctl/internal/agent"
	"github.com/bermudi/nelsonctl/internal/pipeline"
)

const (
	channelCap = 1024
	bufSize    = 64 * 1024
)

type TraceWriter struct {
	path      string
	file      *os.File
	writer    *bufio.Writer
	ch        chan interface{}
	done      chan struct{}
	failed    bool
	failMu    sync.RWMutex
	closeOnce sync.Once
	closed    bool
	closedMu  sync.Mutex
}

func New(path string, meta RunMetaEvent) (*TraceWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, fmt.Errorf("create trace file: %w", err)
	}

	writer := bufio.NewWriterSize(file, bufSize)

	meta.Type = "run_meta"
	if err := json.NewEncoder(writer).Encode(meta); err != nil {
		file.Close()
		return nil, fmt.Errorf("write run_meta: %w", err)
	}
	if err := writer.Flush(); err != nil {
		file.Close()
		return nil, fmt.Errorf("flush run_meta: %w", err)
	}

	tw := &TraceWriter{
		path:   path,
		file:   file,
		writer: writer,
		ch:     make(chan interface{}, channelCap),
		done:   make(chan struct{}),
	}

	go tw.process()

	return tw, nil
}

func (tw *TraceWriter) Send(msg interface{}) {
	if msg == nil {
		return
	}
	tw.closedMu.Lock()
	closed := tw.closed
	tw.closedMu.Unlock()
	if closed {
		return
	}

	tw.failMu.RLock()
	failed := tw.failed
	tw.failMu.RUnlock()
	if failed {
		return
	}
	select {
	case tw.ch <- msg:
	default:
	}
}

func (tw *TraceWriter) Close(runEnd RunEndEvent) error {
	tw.closedMu.Lock()
	if tw.closed {
		tw.closedMu.Unlock()
		return nil
	}
	tw.closed = true
	tw.closedMu.Unlock()

	tw.closeOnce.Do(func() {
		close(tw.ch)
		<-tw.done
	})

	tw.failMu.RLock()
	failed := tw.failed
	tw.failMu.RUnlock()

	runEnd.Type = "run_end"
	runEnd.Ts = timestamp()

	if !failed {
		if err := json.NewEncoder(tw.writer).Encode(runEnd); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR trace: write run_end: %v\n", err)
		}
		if err := tw.writer.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR trace: flush: %v\n", err)
		}
		if err := tw.file.Sync(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR trace: sync: %v\n", err)
		}
	}

	if err := tw.file.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	return nil
}

func (tw *TraceWriter) process() {
	defer close(tw.done)
	enc := json.NewEncoder(tw.writer)
	for msg := range tw.ch {
		tw.failMu.RLock()
		failed := tw.failed
		tw.failMu.RUnlock()
		if failed {
			continue
		}

		traceEvent := tw.toTraceEvent(msg)
		if traceEvent == nil {
			continue
		}

		if err := enc.Encode(traceEvent); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR trace: write event: %v\n", err)
			tw.failMu.Lock()
			tw.failed = true
			tw.failMu.Unlock()
			continue
		}
	}

	tw.failMu.RLock()
	failed := tw.failed
	tw.failMu.RUnlock()

	if !failed {
		if err := tw.writer.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR trace: final flush: %v\n", err)
		}
	}
}

func (tw *TraceWriter) toTraceEvent(msg interface{}) interface{} {
	switch e := msg.(type) {
	case pipeline.StateEvent:
		return StateChangeEvent{
			Type:         "state_change",
			State:        string(e.State),
			BranchReused: e.BranchReused,
			Ts:           timestamp(),
		}
	case pipeline.PhaseStartEvent:
		return PhaseStartEvent{
			Type:    "phase_start",
			Phase:   e.Number,
			Name:    e.Name,
			Attempt: e.Attempt,
			Ts:      timestamp(),
		}
	case pipeline.OutputEvent:
		return OutputChunkEvent{
			Type:  "output_chunk",
			Chunk: e.Chunk,
			Ts:    timestamp(),
		}
	case pipeline.ExecutionContextEvent:
		return ExecutionContextEvent{
			Type:    "execution_context",
			Mode:    string(e.Mode),
			Agent:   e.Agent,
			Step:    e.Step,
			Model:   e.Model,
			Resumed: e.Resumed,
			Ts:      timestamp(),
		}
	case pipeline.ControllerActivityEvent:
		return ControllerActivityEvent{
			Type:      "controller_activity",
			Tool:      e.Tool,
			Summary:   e.Summary,
			Analyzing: e.Analyzing,
			Ts:        timestamp(),
		}
	case pipeline.TauntEvent:
		return TauntEvent{
			Type:  "taunt",
			Phase: e.PhaseNumber,
			Ts:    timestamp(),
		}
	case pipeline.SummaryEvent:
		return SummaryEvent{
			Type:            "summary",
			PhasesCompleted: e.PhasesCompleted,
			PhasesFailed:    e.PhasesFailed,
			TotalAttempts:   e.TotalAttempts,
			Duration:        e.Duration,
			Branch:          e.Branch,
			Mode:            string(e.Mode),
			Resumed:         e.Resumed,
			Ts:              timestamp(),
		}
	case pipeline.PhaseResultEvent:
		return PhaseResultEvent{
			Type:     "phase_result",
			Phase:    e.Number,
			Passed:   e.Passed,
			Attempts: e.Attempts,
			Review:   e.Review,
			Ts:       timestamp(),
		}
	case pipeline.AgentInvokeEvent:
		return AgentInvokeEvent{
			Type:      "agent_invoke",
			Agent:     e.Agent,
			Step:      e.Step,
			Model:     e.Model,
			SessionID: e.SessionID,
			Prompt:    e.Prompt,
			WorkDir:   e.WorkDir,
			Ts:        timestamp(),
		}
	case pipeline.AgentResultEvent:
		return AgentResultEvent{
			Type:       "agent_result",
			ExitCode:   e.ExitCode,
			DurationMs: e.DurationMs,
			StdoutLen:  e.StdoutLen,
			StderrLen:  e.StderrLen,
			Ts:         timestamp(),
		}
	case pipeline.ReviewResultEvent:
		return ReviewResultEvent{
			Type:    "review_result",
			Passed:  e.Passed,
			Output:  e.Output,
			Attempt: e.Attempt,
			Step:    e.Step,
			Phase:   e.Phase,
			Ts:      timestamp(),
		}
	case pipeline.GitCommitEvent:
		return GitCommitEvent{
			Type:    "git_commit",
			Subject: e.Subject,
			Files:   e.Files,
			Ts:      timestamp(),
		}
	case pipeline.GitPushEvent:
		return GitPushEvent{
			Type:   "git_push",
			Remote: e.Remote,
			Branch: e.Branch,
			Ts:     timestamp(),
		}
	case pipeline.PREvent:
		return PREvent{
			Type:  "pr_created",
			Title: e.Title,
			URL:   e.URL,
			Ts:    timestamp(),
		}
	case agent.Event:
		if e.TracePayload == nil {
			return nil
		}
		switch payload := e.TracePayload.(type) {
		case agent.SessionCreatedEvent:
			return SessionCreatedTraceEvent{
				Type:          "session_created",
				SessionID:     payload.SessionID,
				SessionType:   payload.SessionType,
				ParentSession: payload.ParentSession,
				Ts:            timestamp(),
			}
		case agent.SessionSwitchedEvent:
			return SessionSwitchedTraceEvent{
				Type:      "session_switched",
				SessionID: payload.SessionID,
				Ts:        timestamp(),
			}
		case agent.ModelSetEvent:
			return ModelSetTraceEvent{
				Type:     "model_set",
				Provider: payload.Provider,
				Model:    payload.Model,
				Success:  payload.Success,
				Ts:       timestamp(),
			}
		case agent.RPCRawEvent:
			return RPCRawTraceEvent{
				Type:       "rpc_event",
				RPCType:    payload.RPCType,
				StopReason: payload.StopReason,
				SessionID:  payload.SessionID,
				Ts:         timestamp(),
			}
		case agent.EventsDrainedEvent:
			return EventsDrainedTraceEvent{
				Type:  "events_drained",
				Count: payload.Count,
				Ts:    timestamp(),
			}
		case agent.AgentRestartedEvent:
			return AgentRestartedTraceEvent{
				Type:  "agent_restarted",
				Cause: payload.Cause,
				Ts:    timestamp(),
			}
		default:
			return nil
		}
	default:
		return nil
	}
}
