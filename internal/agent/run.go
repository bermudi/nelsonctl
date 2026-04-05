package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

type streamWriter struct {
	builder  *strings.Builder
	callback StreamCallback
}

func (w streamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if w.builder != nil {
		_, _ = w.builder.Write(p)
	}
	if w.callback != nil {
		chunk := append([]byte(nil), p...)
		w.callback(chunk)
	}

	return len(p), nil
}

func runCommand(
	ctx context.Context,
	binary string,
	args []string,
	workDir string,
	timeout time.Duration,
	terminationGrace time.Duration,
	stdoutCallback StreamCallback,
) (*Result, error) {
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", binary, err)
	}

	started := time.Now()
	stdoutBuf := &strings.Builder{}
	stderrBuf := &strings.Builder{}
	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, err := io.Copy(streamWriter{builder: stdoutBuf, callback: stdoutCallback}, stdoutPipe)
		stdoutDone <- err
	}()
	go func() {
		defer wg.Done()
		_, err := io.Copy(stderrBuf, stderrPipe)
		stderrDone <- err
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	waitErr, timedOut := waitForCommand(runCtx, cmd, waitCh, terminationGrace)
	wg.Wait()

	stdoutErr := <-stdoutDone
	stderrErr := <-stderrDone

	result := &Result{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		Duration: time.Since(started),
		ExitCode: exitCode(cmd),
	}

	if !timedOut && waitErr == nil {
		if err := firstNonEOFError(stdoutErr, stderrErr); err != nil {
			return nil, err
		}
	}

	if timedOut {
		if runCtx.Err() != nil {
			return result, fmt.Errorf("%s timed out after %s: %w", binary, timeout, runCtx.Err())
		}
		return result, fmt.Errorf("%s timed out after %s", binary, timeout)
	}
	if waitErr != nil {
		return result, fmt.Errorf("%s failed: %w", binary, waitErr)
	}

	return result, nil
}

func waitForCommand(ctx context.Context, cmd *exec.Cmd, waitCh <-chan error, grace time.Duration) (error, bool) {
	select {
	case err := <-waitCh:
		return err, false
	case <-ctx.Done():
		_ = signalProcessGroup(cmd, syscall.SIGTERM)
		if grace <= 0 {
			_ = signalProcessGroup(cmd, syscall.SIGKILL)
			return <-waitCh, true
		}

		timer := time.NewTimer(grace)
		defer timer.Stop()

		select {
		case err := <-waitCh:
			return err, true
		case <-timer.C:
			_ = signalProcessGroup(cmd, syscall.SIGKILL)
			return <-waitCh, true
		}
	}
}

func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}

	if err := syscall.Kill(-cmd.Process.Pid, sig); err == nil {
		return nil
	}

	return cmd.Process.Signal(sig)
}

func exitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

func firstNonEOFError(errs ...error) error {
	for _, err := range errs {
		if err == nil || errors.Is(err, io.EOF) {
			continue
		}
		return err
	}
	return nil
}
