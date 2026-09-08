//go:build windows

package job

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

func hide(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func replaceFile(from, to string) error {
	a, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	r, _, e := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x1|0x8)
	if r == 0 {
		return e
	}
	return nil
}

func (m Manager) Check() error {
	if err := m.validate(); err != nil {
		return err
	}
	_, err := command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; $s=New-Object -ComObject 'Schedule.Service'; $s.Connect(); $null=$s.GetFolder('\')`)
	if err != nil {
		return fmt.Errorf("Task Scheduler unavailable for the current user: %w", err)
	}
	return nil
}
func windowsQuote(s string) string { return syscall.EscapeArg(s) }
func (m Manager) taskXML(exe, sid string, now time.Time) string {
	// wscript starts the check with SW_HIDE. Task Hidden alone only hides the
	// entry in Task Scheduler; it does not suppress console creation.
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
 <RegistrationInfo><Description>AgentWarmup guarded minute check</Description></RegistrationInfo>
 <Triggers><TimeTrigger><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition><StartBoundary>%s</StartBoundary><Enabled>true</Enabled></TimeTrigger><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId></LogonTrigger></Triggers>
 <Principals><Principal id="Author"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
 <Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><StartWhenAvailable>true</StartWhenAvailable><RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable><Enabled>true</Enabled><WakeToRun>false</WakeToRun><ExecutionTimeLimit>PT5M</ExecutionTimeLimit></Settings>
 <Actions Context="Author"><Exec><Command>%s</Command><Arguments>%s</Arguments></Exec></Actions>
</Task>`, now.Format("2006-01-02T15:04:05"), escapeXML(sid), escapeXML(sid), escapeXML(filepath.Join(os.Getenv("SystemRoot"), "System32", "wscript.exe")), escapeXML("//B //Nologo "+windowsQuote(filepath.Join(m.Root, "bin", "check.vbs"))))
}
func (m Manager) Install(exe string) error {
	if err := m.validateExe(exe); err != nil {
		return err
	}
	if err := m.Check(); err != nil {
		return err
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	cmd := windowsQuote(exe) + " check --home " + windowsQuote(m.Root)
	script := "Option Explicit\r\nDim shell, result\r\nSet shell = CreateObject(\"WScript.Shell\")\r\nresult = shell.Run(\"" + strings.ReplaceAll(cmd, "\"", "\"\"") + "\", 0, True)\r\nWScript.Quit result\r\n"
	if err = writeFile(filepath.Join(m.Root, "bin", "check.vbs"), utf16LE(script)); err != nil {
		return err
	}
	f, err := os.CreateTemp("", "agentwarmup-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(utf16LE(m.taskXML(exe, u.Uid, time.Now())))
	ce := f.Close()
	if err != nil {
		return err
	}
	if ce != nil {
		return ce
	}
	_, err = command("schtasks.exe", "/Create", "/F", "/TN", m.Name, "/XML", f.Name())
	return err
}
func (m Manager) Inspect() (Status, error) {
	if err := m.validate(); err != nil {
		return Status{}, err
	}
	// The COM error code is locale-independent. All other errors remain errors,
	// rather than being mistaken for a missing task.
	script := `$ErrorActionPreference='Stop'; $s=New-Object -ComObject 'Schedule.Service'; $s.Connect(); try { $t=$s.GetFolder('\').GetTask('` + m.Name + `'); @{Installed=$true;Enabled=[bool]$t.Enabled;Detail='Task Scheduler'} | ConvertTo-Json -Compress } catch { if ($_.Exception.HResult -eq -2147024894) { '{"Installed":false,"Enabled":false}' } else { throw } }`
	out, err := command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return Status{}, err
	}
	var s Status
	err = json.Unmarshal(out, &s)
	return s, err
}
func (m Manager) Uninstall() error {
	s, err := m.Inspect()
	if err != nil {
		return err
	}
	if !s.Installed {
		return nil
	}
	_, err = command("schtasks.exe", "/Delete", "/F", "/TN", m.Name)
	return err
}
func (m Manager) SetEnabled(enabled bool) error {
	if e := m.validate(); e != nil {
		return e
	}
	flag := "/Disable"
	if enabled {
		flag = "/Enable"
	}
	_, e := command("schtasks.exe", "/Change", "/TN", m.Name, flag)
	return e
}
func utf16LE(s string) []byte {
	units := utf16.Encode([]rune(s))
	buf := make([]byte, 2+len(units)*2)
	buf[0] = 0xff
	buf[1] = 0xfe
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[2+i*2:], u)
	}
	return buf
}
