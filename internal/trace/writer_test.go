package trace

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bermudi/nelsonctl/internal/pipeline"
)

func TestEventSerialization(t *testing.T) {
	ts := "2026-04-05T23:45:00.123456789Z"

	stateChange := StateChangeEvent{Type: "state_change", State: "PhaseLoop", Ts: ts}
	data, err := json.Marshal(stateChange)
	if err != nil {
		t.Fatalf("marshal state_change: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal state_change: %v", err)
	}
	if parsed["type"] != "state_change" || parsed["state"] != "PhaseLoop" || parsed["ts"] != ts {
		t.Errorf("unexpected fields: %v", parsed)
	}

	phaseStart := PhaseStartEvent{Type: "phase_start", Phase: 1, Name: "impl", Attempt: 1, Ts: ts}
	data, err = json.Marshal(phaseStart)
	if err != nil {
		t.Fatalf("marshal phase_start: %v", err)
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal phase_start: %v", err)
	}
	if parsed["type"] != "phase_start" || parsed["phase"] != float64(1) {
		t.Errorf("unexpected fields: %v", parsed)
	}

	outputChunk := OutputChunkEvent{Type: "output_chunk", Chunk: "hello world", Ts: ts}
	data, err = json.Marshal(outputChunk)
	if err != nil {
		t.Fatalf("marshal output_chunk: %v", err)
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal output_chunk: %v", err)
	}
	if parsed["chunk"] != "hello world" {
		t.Errorf("unexpected chunk: %v", parsed["chunk"])
	}
}

func TestFilePermissions(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "test",
		Agent:     "test-agent",
		Change:    "test-change",
		Hostname:  "localhost",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected 0600, got %o", perm)
	}

	tw.Close(RunEndEvent{Status: "completed", DurationMs: 100, PhasesCompleted: 1, PhasesFailed: 0})
}

func TestErrorIsolation(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "test",
		Agent:     "test-agent",
		Change:    "test-change",
		Hostname:  "localhost",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tw.Send(pipeline.StateEvent{State: pipeline.StateBranch})

	os.Chmod(path, 0000)

	tw.Send(pipeline.StateEvent{State: pipeline.StateInit})

	tw.Close(RunEndEvent{Status: "error", DurationMs: 100, PhasesCompleted: 0, PhasesFailed: 1})
}

func TestChannelOverflow(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "test",
		Agent:     "test-agent",
		Change:    "test-change",
		Hostname:  "localhost",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < channelCap+500; i++ {
		tw.Send(pipeline.StateEvent{State: pipeline.StateInit})
	}

	tw.Close(RunEndEvent{Status: "completed", DurationMs: 100, PhasesCompleted: 1, PhasesFailed: 0})

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	count := 0
	for _, line := range lines {
		if line != "" {
			count++
		}
	}

	if count != channelCap+2 {
		t.Logf("expected %d lines (cap + meta + end), got %d", channelCap+2, count)
	}
}

func TestPartialTraceValidity(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "test",
		Agent:     "test-agent",
		Change:    "test-change",
		Hostname:  "localhost",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	events := []pipeline.Event{
		pipeline.StateEvent{State: pipeline.StateInit},
		pipeline.StateEvent{State: pipeline.StateBranch},
		pipeline.PhaseStartEvent{Number: 1, Name: "test", Attempt: 1},
		pipeline.OutputEvent{Chunk: "hello"},
		pipeline.OutputEvent{Chunk: "world"},
		pipeline.PhaseResultEvent{Number: 1, Passed: true, Attempts: 1, Review: "ok"},
		pipeline.TauntEvent{PhaseNumber: 1},
		pipeline.SummaryEvent{PhasesCompleted: 1, PhasesFailed: 0, Duration: "1s", Branch: "main"},
	}
	for _, e := range events {
		tw.Send(e)
	}

	tw.Close(RunEndEvent{Status: "completed", DurationMs: 1000, PhasesCompleted: 1, PhasesFailed: 0})

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Errorf("line %d: invalid JSON: %v\n%s", i, err, line)
		}
		if _, ok := parsed["type"]; !ok {
			t.Errorf("line %d: missing type field", i)
		}
		if _, ok := parsed["ts"]; !ok && parsed["type"] != "run_meta" {
			t.Errorf("line %d: missing ts field", i)
		}
	}
}

func TestReviewResultEventConditionalPhase(t *testing.T) {
	ts := "2026-04-05T23:45:00.123456789Z"

	phaseReview := ReviewResultEvent{
		Type:    "review_result",
		Passed:  true,
		Output:  "looks good",
		Attempt: 2,
		Step:    "phase",
		Phase:   1,
		Ts:      ts,
	}
	data, err := json.Marshal(phaseReview)
	if err != nil {
		t.Fatalf("marshal phase review: %v", err)
	}
	var parsedPhase map[string]interface{}
	if err := json.Unmarshal(data, &parsedPhase); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsedPhase["step"] != "phase" {
		t.Errorf("expected step=phase, got %v", parsedPhase["step"])
	}
	if parsedPhase["phase"] != float64(1) {
		t.Errorf("expected phase=1 for phase step, got %v", parsedPhase["phase"])
	}

	finalReview := ReviewResultEvent{
		Type:    "review_result",
		Passed:  false,
		Output:  "needs work",
		Attempt: 1,
		Step:    "final",
		Ts:      ts,
	}
	data, err = json.Marshal(finalReview)
	if err != nil {
		t.Fatalf("marshal final review: %v", err)
	}
	var parsedFinal map[string]interface{}
	if err := json.Unmarshal(data, &parsedFinal); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsedFinal["step"] != "final" {
		t.Errorf("expected step=final, got %v", parsedFinal["step"])
	}
	if _, exists := parsedFinal["phase"]; exists {
		t.Errorf("expected no phase field for final step, got %v", parsedFinal["phase"])
	}
}

func TestRunMetaEvent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "v1.0.0",
		Agent:     "opencode",
		Change:    "my-feature",
		Hostname:  "buildbox",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tw.Close(RunEndEvent{Status: "completed", DurationMs: 5000, PhasesCompleted: 2, PhasesFailed: 0})

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var firstLine RunMetaEvent
	if err := json.NewDecoder(f).Decode(&firstLine); err != nil {
		t.Fatalf("decode first line: %v", err)
	}
	if firstLine.Type != "run_meta" {
		t.Errorf("expected type=run_meta, got %s", firstLine.Type)
	}
	if firstLine.Version != "v1.0.0" {
		t.Errorf("expected version=v1.0.0, got %s", firstLine.Version)
	}
	if firstLine.Agent != "opencode" {
		t.Errorf("expected agent=opencode, got %s", firstLine.Agent)
	}
}

func TestCloseIdempotent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "test",
		Agent:     "test",
		Change:    "test",
		Hostname:  "localhost",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tw.Send(pipeline.StateEvent{State: pipeline.StateInit})

	err1 := tw.Close(RunEndEvent{Status: "completed", DurationMs: 100, PhasesCompleted: 1, PhasesFailed: 0})
	err2 := tw.Close(RunEndEvent{Status: "completed", DurationMs: 100, PhasesCompleted: 1, PhasesFailed: 0})

	if err1 != nil {
		t.Errorf("first close error: %v", err1)
	}
	if err2 != nil {
		t.Errorf("second close error: %v", err2)
	}
}

func TestSendAfterClose(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "test",
		Agent:     "test",
		Change:    "test",
		Hostname:  "localhost",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tw.Close(RunEndEvent{Status: "completed", DurationMs: 100, PhasesCompleted: 1, PhasesFailed: 0})

	tw.Send(pipeline.StateEvent{State: pipeline.StateInit})
}

func TestToTraceEventExistingEvents(t *testing.T) {
	tw := &TraceWriter{}

	stateEv := pipeline.StateEvent{State: pipeline.StateBranch}
	result := tw.toTraceEvent(stateEv)
	traceEv, ok := result.(StateChangeEvent)
	if !ok {
		t.Fatalf("expected StateChangeEvent")
	}
	if traceEv.Type != "state_change" || traceEv.State != "Branch" {
		t.Errorf("unexpected: %+v", traceEv)
	}

	phaseStartEv := pipeline.PhaseStartEvent{Number: 1, Name: "impl", Attempt: 2}
	result = tw.toTraceEvent(phaseStartEv)
	tracePs, ok := result.(PhaseStartEvent)
	if !ok {
		t.Fatalf("expected PhaseStartEvent")
	}
	if tracePs.Phase != 1 || tracePs.Name != "impl" || tracePs.Attempt != 2 {
		t.Errorf("unexpected: %+v", tracePs)
	}

	outputEv := pipeline.OutputEvent{Chunk: "hello world"}
	result = tw.toTraceEvent(outputEv)
	traceOut, ok := result.(OutputChunkEvent)
	if !ok {
		t.Fatalf("expected OutputChunkEvent")
	}
	if traceOut.Chunk != "hello world" {
		t.Errorf("unexpected: %+v", traceOut)
	}

	phaseResultEv := pipeline.PhaseResultEvent{Number: 1, Passed: true, Attempts: 3, Review: "ok"}
	result = tw.toTraceEvent(phaseResultEv)
	tracePr, ok := result.(PhaseResultEvent)
	if !ok {
		t.Fatalf("expected PhaseResultEvent")
	}
	if tracePr.Phase != 1 || !tracePr.Passed || tracePr.Attempts != 3 {
		t.Errorf("unexpected: %+v", tracePr)
	}

	tauntEv := pipeline.TauntEvent{PhaseNumber: 2}
	result = tw.toTraceEvent(tauntEv)
	traceT, ok := result.(TauntEvent)
	if !ok {
		t.Fatalf("expected TauntEvent")
	}
	if traceT.Phase != 2 {
		t.Errorf("unexpected: %+v", traceT)
	}

	summaryEv := pipeline.SummaryEvent{PhasesCompleted: 3, PhasesFailed: 1, Duration: "5m", Branch: "change/foo"}
	result = tw.toTraceEvent(summaryEv)
	traceS, ok := result.(SummaryEvent)
	if !ok {
		t.Fatalf("expected SummaryEvent")
	}
	if traceS.PhasesCompleted != 3 || traceS.PhasesFailed != 1 || traceS.Duration != "5m" || traceS.Branch != "change/foo" {
		t.Errorf("unexpected: %+v", traceS)
	}

	nilResult := tw.toTraceEvent(nil)
	if nilResult != nil {
		t.Errorf("expected nil for nil input")
	}

	unknownResult := tw.toTraceEvent("unknown")
	if unknownResult != nil {
		t.Errorf("expected nil for unknown input")
	}
}

func TestRunEndEvent(t *testing.T) {
	ts := "2026-04-05T23:45:00.123456789Z"

	completed := RunEndEvent{
		Type:            "run_end",
		Status:          "completed",
		DurationMs:      60000,
		PhasesCompleted: 3,
		PhasesFailed:    0,
		Ts:              ts,
	}
	data, err := json.Marshal(completed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["status"] != "completed" {
		t.Errorf("expected status=completed, got %v", parsed["status"])
	}
	if parsed["duration_ms"] != float64(60000) {
		t.Errorf("expected duration_ms=60000, got %v", parsed["duration_ms"])
	}

	errored := RunEndEvent{
		Type:            "run_end",
		Status:          "error",
		DurationMs:      30000,
		PhasesCompleted: 1,
		PhasesFailed:    1,
		Error:           "branch conflict",
		Ts:              ts,
	}
	data, err = json.Marshal(errored)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["error"] != "branch conflict" {
		t.Errorf("expected error='branch conflict', got %v", parsed["error"])
	}

	interrupted := RunEndEvent{
		Type:            "run_end",
		Status:          "interrupted",
		DurationMs:      15000,
		PhasesCompleted: 2,
		PhasesFailed:    0,
		Ts:              ts,
	}
	data, err = json.Marshal(interrupted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["status"] != "interrupted" {
		t.Errorf("expected status=interrupted, got %v", parsed["status"])
	}
}

func TestGitCommitEvent(t *testing.T) {
	ts := "2026-04-05T23:45:00.123456789Z"

	evt := GitCommitEvent{
		Type:    "git_commit",
		Subject: "feat: add login",
		Files:   []string{"src/auth.go", "src/login.go"},
		Ts:      ts,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["subject"] != "feat: add login" {
		t.Errorf("unexpected subject: %v", parsed["subject"])
	}
	files, ok := parsed["files"].([]interface{})
	if !ok {
		t.Fatalf("files is not array")
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d", len(files))
	}
}

func TestGitPushEvent(t *testing.T) {
	ts := "2026-04-05T23:45:00.123456789Z"

	evt := GitPushEvent{
		Type:   "git_push",
		Remote: "origin",
		Branch: "change/my-feature",
		Ts:     ts,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["remote"] != "origin" || parsed["branch"] != "change/my-feature" {
		t.Errorf("unexpected: %v", parsed)
	}
}

func TestPREvent(t *testing.T) {
	ts := "2026-04-05T23:45:00.123456789Z"

	evt := PREvent{
		Type:  "pr_created",
		Title: "feat: add login flow",
		URL:   "https://github.com/owner/repo/pull/123",
		Ts:    ts,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["title"] != "feat: add login flow" || parsed["url"] != "https://github.com/owner/repo/pull/123" {
		t.Errorf("unexpected: %v", parsed)
	}
}

func TestTraceFileContent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "trace.jsonl")

	meta := RunMetaEvent{
		Version:   "test",
		Agent:     "test-agent",
		Change:    "test-change",
		Hostname:  "localhost",
		StartedAt: "2026-04-05T23:45:00Z",
	}
	tw, err := New(path, meta)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	events := []pipeline.Event{
		pipeline.StateEvent{State: pipeline.StateInit},
		pipeline.StateEvent{State: pipeline.StateBranch},
		pipeline.PhaseStartEvent{Number: 1, Name: "test", Attempt: 1},
		pipeline.OutputEvent{Chunk: "hello"},
		pipeline.PhaseResultEvent{Number: 1, Passed: true, Attempts: 1, Review: "ok"},
	}
	for _, e := range events {
		tw.Send(e)
	}

	tw.Close(RunEndEvent{Status: "completed", DurationMs: 1000, PhasesCompleted: 1, PhasesFailed: 0})

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		t.Fatalf("scan error: %v", err)
	}

	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines, got %d", len(lines))
	}

	firstLine := lines[0]
	var first RunMetaEvent
	if err := json.Unmarshal([]byte(firstLine), &first); err != nil {
		t.Fatalf("unmarshal first line: %v", err)
	}
	if first.Type != "run_meta" {
		t.Errorf("expected first line type=run_meta, got %s", first.Type)
	}

	lastLine := lines[len(lines)-1]
	var last RunEndEvent
	if err := json.Unmarshal([]byte(lastLine), &last); err != nil {
		t.Fatalf("unmarshal last line: %v", err)
	}
	if last.Type != "run_end" {
		t.Errorf("expected last line type=run_end, got %s", last.Type)
	}
	if last.Status != "completed" {
		t.Errorf("expected status=completed, got %s", last.Status)
	}
}
