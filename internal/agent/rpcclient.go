package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type processStarter func(ctx context.Context, workDir string, env []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error)

type rpcTransport interface {
	Start(ctx context.Context) error
	Close() error
	Events() <-chan rpcEvent
	Send(ctx context.Context, command rpcCommand) (rpcResponse, error)
	SendNoResponse(command rpcCommand) error
}

type rpcClient struct {
	workDir        string
	env            []string
	starter        processStarter
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	stderr         io.ReadCloser
	responses      map[string]chan rpcResponse
	events         *queuedChannel[rpcEvent]
	closed         chan struct{}
	requestCounter uint64
	mu             sync.Mutex
	writesMu       sync.Mutex
	stderrBuf      strings.Builder
	readersWG      sync.WaitGroup
	termination    time.Duration
}

func newRPCClient(workDir string, env []string) *rpcClient {
	return &rpcClient{
		workDir:     workDir,
		env:         env,
		starter:     startPiRPCProcess,
		responses:   map[string]chan rpcResponse{},
		events:      newQueuedChannel[rpcEvent](256),
		closed:      make(chan struct{}),
		termination: time.Second,
	}
}

func startPiRPCProcess(ctx context.Context, workDir string, env []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	return startPiRPCProcessWithArgs(ctx, workDir, env, []string{"--mode", "rpc", "--no-extensions"})
}

func startPiRPCProcessWithArgs(ctx context.Context, workDir string, env []string, args []string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "pi", append([]string(nil), args...)...)
	cmd.Dir = workDir
	if len(env) > 0 {
		cmd.Env = append([]string(nil), env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, err
	}
	return cmd, stdin, stdout, stderr, nil
}

func (c *rpcClient) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil {
		return fmt.Errorf("rpc client already started")
	}
	cmd, stdin, stdout, stderr, err := c.starter(ctx, c.workDir, c.env)
	if err != nil {
		return fmt.Errorf("start pi rpc process: %w", err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	c.closed = make(chan struct{})
	c.readersWG.Add(2)
	go c.readStdout()
	go c.readStderr()
	go c.watchProcessExit()
	return nil
}

func (c *rpcClient) Close() error {
	c.mu.Lock()
	if c.cmd == nil {
		c.mu.Unlock()
		return nil
	}
	cmd := c.cmd
	c.cmd = nil
	stdin := c.stdin
	c.stdin = nil
	c.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	_ = signalProcessGroup(cmd, syscall.SIGTERM)
	select {
	case <-c.closed:
	case <-time.After(c.termination):
		_ = signalProcessGroup(cmd, syscall.SIGKILL)
	}
	c.readersWG.Wait()
	c.events.Close()
	return nil
}

func (c *rpcClient) Events() <-chan rpcEvent {
	return c.events.Channel()
}

func (c *rpcClient) Send(ctx context.Context, command rpcCommand) (rpcResponse, error) {
	if command.Type == "extension_ui_response" {
		return rpcResponse{}, c.SendNoResponse(command)
	}
	id := "req_" + strconv.FormatUint(atomic.AddUint64(&c.requestCounter, 1), 10)
	command.ID = id
	data, err := json.Marshal(command)
	if err != nil {
		return rpcResponse{}, fmt.Errorf("marshal rpc command: %w", err)
	}
	responseCh := make(chan rpcResponse, 1)
	c.mu.Lock()
	c.responses[id] = responseCh
	stdin := c.stdin
	c.mu.Unlock()
	if stdin == nil {
		return rpcResponse{}, fmt.Errorf("rpc client not started")
	}

	c.writesMu.Lock()
	_, err = stdin.Write(append(data, '\n'))
	c.writesMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.responses, id)
		c.mu.Unlock()
		return rpcResponse{}, fmt.Errorf("write rpc command: %w", err)
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.responses, id)
		c.mu.Unlock()
		return rpcResponse{}, ctx.Err()
	case response := <-responseCh:
		if !response.Success {
			return response, fmt.Errorf("rpc %s failed: %s", response.Command, response.Error)
		}
		return response, nil
	}
}

func (c *rpcClient) SendNoResponse(command rpcCommand) error {
	data, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("marshal rpc command: %w", err)
	}
	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()
	if stdin == nil {
		return fmt.Errorf("rpc client not started")
	}
	c.writesMu.Lock()
	_, err = stdin.Write(append(data, '\n'))
	c.writesMu.Unlock()
	if err != nil {
		return fmt.Errorf("write rpc command: %w", err)
	}
	return nil
}

func (c *rpcClient) readStdout() {
	defer c.readersWG.Done()
	defer close(c.closed)
	defer c.events.Close() // signal that no more events will come

	reader := bufio.NewReader(c.stdout)
	for {
		line, err := readJSONLRecord(reader)
		if err != nil {
			if err == io.EOF {
				return
			}
			continue
		}
		if len(line) == 0 {
			continue
		}

		var envelope rpcEventEnvelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		if envelope.Type == "response" {
			var response rpcResponse
			if err := json.Unmarshal(line, &response); err != nil {
				continue
			}
			c.mu.Lock()
			ch := c.responses[response.ID]
			delete(c.responses, response.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- response
			}
			continue
		}
		if os.Getenv("NELSONCTL_DEBUG") != "" && envelope.Type != "response" {
			fmt.Fprintf(os.Stderr, "[DEBUG-rpc] raw: %s\n", string(line))
		}
		var event rpcEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		event.Raw = append(event.Raw[:0], line...)
		c.events.Send(event)
	}
}

func readJSONLRecord(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if err == io.EOF {
			if len(line) == 0 {
				return nil, io.EOF
			}
		} else {
			return nil, err
		}
	}

	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, nil
}

func (c *rpcClient) readStderr() {
	defer c.readersWG.Done()
	reader := bufio.NewReader(c.stderr)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			c.mu.Lock()
			c.stderrBuf.WriteString(line)
			c.mu.Unlock()
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
	}
}

func (c *rpcClient) Stderr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stderrBuf.String()
}

func envToArgs(env []string) []string {
	_ = env
	return nil
}

// watchProcessExit waits for the pi process to exit, then fails all pending
// RPC responses. This prevents Send() from blocking forever when pi dies
// before sending a response.
func (c *rpcClient) watchProcessExit() {
	<-c.closed // closed by readStdout when stdout pipe closes
	c.mu.Lock()
	for id, ch := range c.responses {
		ch <- rpcResponse{ID: id, Success: false, Error: "pi process exited"}
		delete(c.responses, id)
	}
	c.mu.Unlock()
}
