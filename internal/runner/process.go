package runner

import (
	"bytes"
	"context"
	"os/exec"
	"sync"

	"github.com/OliverGrabner/agentwarmup/internal/provider"
)

const outputLimit = 64 * 1024

type boundedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if n > outputLimit-b.buf.Len() {
		b.overflow = true
	}
	if remaining := outputLimit - b.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.buf.Write(p)
	}
	return n, nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func Execute(ctx context.Context, inv provider.Invocation) provider.Result {
	if ctx.Err() != nil {
		return provider.Result{Outcome: "interrupted", Detail: "Request cancelled before launch."}
	}
	cmd := exec.Command(inv.Executable, inv.Args...)
	cmd.Dir, cmd.Env = inv.Dir, inv.Env
	// A nil Stdin is a closed empty stream, never the setup terminal.
	var stdout, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cleanup, err := startProcess(cmd)
	if err != nil {
		return provider.Result{Outcome: "failed", Detail: provider.Scrub(err.Error())}
	}
	defer cleanup()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if ctx.Err() != nil {
			outcome := "timeout"
			if ctx.Err() == context.Canceled {
				outcome = "interrupted"
			}
			return provider.Result{Outcome: outcome, Detail: "Request stopped before completion."}
		}
		code := 0
		if err != nil {
			code = -1
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
		}
		if stdout.overflow || stderr.overflow {
			return provider.Result{Outcome: "failed", Detail: "Provider output exceeded the safe limit."}
		}
		return provider.Classify(code, stdout.String(), stderr.String())
	case <-ctx.Done():
		cleanup()
		_ = cmd.Process.Kill()
		<-done
		outcome := "timeout"
		if ctx.Err() == context.Canceled {
			outcome = "interrupted"
		}
		return provider.Result{Outcome: outcome, Detail: "Request stopped before completion; it will not be retried."}
	}
}
