package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/provider"
)

var fakeOnce sync.Once
var fakeExecutable string
var fakeBuildErr error

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "--hold-test-lock" {
		lock, err := TryLock(os.Args[2])
		if err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Args[3], []byte("locked"), 0600); err != nil {
			os.Exit(3)
		}
		time.Sleep(time.Minute)
		_ = lock.Close()
		os.Exit(0)
	}
	code := m.Run()
	if fakeExecutable != "" {
		_ = os.Remove(fakeExecutable)
		_ = os.Remove(filepath.Dir(fakeExecutable))
	}
	os.Exit(code)
}

func fakeProcess(t *testing.T, mode string, extra ...string) provider.Invocation {
	t.Helper()
	fakeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "agentwarmup-fake-build-")
		if err != nil {
			fakeBuildErr = err
			return
		}
		fakeExecutable = filepath.Join(dir, "fakeprovider")
		if runtime.GOOS == "windows" {
			fakeExecutable += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", fakeExecutable, "../../testdata/fakeprovider")
		out, err := cmd.CombinedOutput()
		if err != nil {
			fakeBuildErr = &buildError{err, string(out)}
		}
	})
	if fakeBuildErr != nil {
		t.Fatal(fakeBuildErr)
	}
	return provider.Invocation{Executable: fakeExecutable, Args: append([]string{"--fake-mode", mode}, extra...), Env: os.Environ(), Dir: t.TempDir()}
}

type buildError struct {
	err    error
	output string
}

func (e *buildError) Error() string { return e.err.Error() + ": " + e.output }

func TestExecuteClosedInputAndLiteralArguments(t *testing.T) {
	record := filepath.Join(t.TempDir(), "record.jsonl")
	inv := fakeProcess(t, "ok", "--fake-record", record, "spaces & % Unicode café")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if result := Execute(ctx, inv); result.Outcome != "succeeded" {
		t.Fatalf("%+v", result)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Args       []string
		Dir        string
		StdinBytes int
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	wantDir, err := filepath.EvalSymlinks(inv.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.StdinBytes != 0 || got.Dir != wantDir || got.Args[len(got.Args)-1] != "spaces & % Unicode café" {
		t.Fatalf("unexpected invocation: %+v", got)
	}
}

func TestExecuteOutputLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := Execute(ctx, fakeProcess(t, "large"))
	if result.Outcome != "failed" || !strings.Contains(result.Detail, "limit") {
		t.Fatalf("%+v", result)
	}
}

func TestExecuteKillsDescendants(t *testing.T) {
	record := filepath.Join(t.TempDir(), "parent")
	inv := fakeProcess(t, "child", "--fake-record", record, "--fake-delay", "900ms")
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	start := time.Now()
	result := Execute(ctx, inv)
	if result.Outcome != "timeout" {
		t.Fatalf("%+v", result)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout did not contain the process tree promptly")
	}
	if _, err := os.Stat(record + ".child"); err != nil {
		t.Fatalf("child never started; test inconclusive: %v", err)
	}
	time.Sleep(time.Second)
	if _, err := os.Stat(record + ".child.survived"); !os.IsNotExist(err) {
		t.Fatal("child survived parent timeout")
	}
}

func TestExecuteCancelledBeforeStart(t *testing.T) {
	record := filepath.Join(t.TempDir(), "unexpected")
	inv := fakeProcess(t, "ok", "--fake-record", record)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := Execute(ctx, inv); result.Outcome != "interrupted" {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatal("cancelled process launched")
	}
}

func TestExecuteProviderFailures(t *testing.T) {
	for mode, want := range map[string]string{"auth": "auth_required", "rate": "rate_limited", "model": "model_unavailable", "structured-error": "rate_limited"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if got := Execute(ctx, fakeProcess(t, mode)); got.Outcome != want {
				t.Fatalf("got %+v, want %s", got, want)
			}
		})
	}
}

func TestKernelLockReleasedAfterProcessCrash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "provider.lock")
	ready := filepath.Join(dir, "ready")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "--hold-test-lock", path, ready)
	cleanup, err := startProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	lock, err := TryLock(path)
	if lock != nil {
		_ = lock.Close()
	}
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second process acquired held lock: %v", err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	lock, err = TryLock(path)
	if err != nil {
		t.Fatalf("crash left stale kernel lock: %v", err)
	}
	_ = lock.Close()
}
