//go:build windows

package runner

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

var createJob = kernel.NewProc("CreateJobObjectW")
var setJobInformation = kernel.NewProc("SetInformationJobObject")
var assignJob = kernel.NewProc("AssignProcessToJobObject")
var resumeProcess = syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess")

type basicLimits struct {
	ProcessTime, JobTime                 int64
	Flags                                uint32
	MinimumWorkingSet, MaximumWorkingSet uintptr
	ActiveProcesses                      uint32
	Affinity                             uintptr
	Priority, Scheduling                 uint32
}

type extendedLimits struct {
	Basic                                                      basicLimits
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

func startProcess(cmd *exec.Cmd) (func(), error) {
	handle, _, err := createJob.Call(0, 0)
	if handle == 0 {
		return nil, fmt.Errorf("create process job: %w", err)
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { _ = syscall.CloseHandle(syscall.Handle(handle)) }) }
	limits := extendedLimits{Basic: basicLimits{Flags: 0x2000}} // Kill descendants when the job closes.
	ok, _, err := setJobInformation.Call(handle, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		cleanup()
		return nil, fmt.Errorf("set process job limit: %w", err)
	}
	// Suspend until the child is assigned to the job, so it cannot escape by spawning first.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000004}
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}
	process, err := syscall.OpenProcess(0x0901, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cleanup()
		return nil, err
	}
	defer syscall.CloseHandle(process)
	ok, _, err = assignJob.Call(handle, uintptr(process))
	if ok == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		cleanup()
		return nil, fmt.Errorf("assign process job: %w", err)
	}
	status, _, _ := resumeProcess.Call(uintptr(process))
	if status != 0 {
		cleanup()
		_ = cmd.Wait()
		return nil, fmt.Errorf("resume process: status 0x%x", status)
	}
	return cleanup, nil
}
