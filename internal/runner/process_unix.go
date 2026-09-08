//go:build linux || darwin

package runner

import (
	"os/exec"
	"sync"
	"syscall"
)

func startProcess(cmd *exec.Cmd) (func(), error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	}, nil
}
