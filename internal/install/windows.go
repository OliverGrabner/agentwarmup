//go:build windows

package install

import (
	"encoding/base64"
	encodingbinary "encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/OliverGrabner/agentwarmup/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

func hide(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func replace(from, to string) error {
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
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func powershell(script string) *exec.Cmd {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		encodingbinary.LittleEndian.PutUint16(b[i*2:], v)
	}
	c := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	hide(c)
	return c
}
func addPath(p config.Paths) error {
	if r, e := readPathRecord(p); e != nil {
		return e
	} else if r.Kind != "" && (r.Kind != "windows" || !samePath(r.Path, filepath.Dir(binary(p)))) {
		return fmt.Errorf("invalid owned PATH record")
	}
	bin := filepath.Dir(binary(p))
	query, e := powershell(`[string][Environment]::GetEnvironmentVariable('Path','User') | ConvertTo-Json -Compress`).Output()
	if e != nil {
		return fmt.Errorf("read user PATH: %w", e)
	}
	var previous string
	if e = json.Unmarshal(query, &previous); e != nil {
		return e
	}
	for _, part := range strings.Split(previous, ";") {
		if samePath(part, bin) {
			return nil
		}
	}
	next := previous
	if next != "" && !strings.HasSuffix(next, ";") {
		next += ";"
	}
	next += bin
	script := `$ErrorActionPreference='Stop'; if ([string][Environment]::GetEnvironmentVariable('Path','User') -cne ` + psQuote(previous) + `) { throw 'User PATH changed during installation; retry installation.' }; [Environment]::SetEnvironmentVariable('Path', ` + psQuote(next) + `, 'User')`
	// Record ownership only after proving the entry was absent.
	if e := savePathRecord(p, pathRecord{Kind: "windows", Path: bin}); e != nil {
		return e
	}
	_, e = powershell(script).CombinedOutput()
	if e != nil {
		return fmt.Errorf("update user PATH: %w", e)
	}
	return nil
}
func removePath(p config.Paths) error {
	r, e := readPathRecord(p)
	if e != nil || r.Kind == "" {
		return e
	}
	if r.Kind != "windows" || !samePath(r.Path, filepath.Dir(binary(p))) {
		return fmt.Errorf("invalid owned PATH record")
	}
	script := `$ErrorActionPreference='Stop'; $entry=` + psQuote(r.Path) + `; $old=[Environment]::GetEnvironmentVariable('Path','User'); $parts=@($old -split ';' | Where-Object { $_ -ne $entry }); [Environment]::SetEnvironmentVariable('Path',($parts -join ';'),'User')`
	if e = powershell(script).Run(); e != nil {
		return fmt.Errorf("remove user PATH entry: %w", e)
	}
	return nil
}
func removeRoot(p config.Paths) error {
	if e := owned(p); e != nil {
		return e
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(p.Root, exe)
	if e != nil {
		return e
	}
	inside := rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
	if !inside {
		return os.RemoveAll(p.Root)
	}
	token, e := os.ReadFile(filepath.Join(p.Root, ".uninstalling"))
	if e != nil {
		return fmt.Errorf("verify deferred uninstall generation: %w", e)
	}
	// Match the generation and acquire the same byte-range operation lock as
	// the CLI. Traverse directories explicitly without following reparse points.
	script := `$ErrorActionPreference='Stop'; $root=` + psQuote(p.Root) + `; $pidToWait=` + fmt.Sprint(os.Getpid()) + `; for ($i=0; $i -lt 120; $i++) { if (-not (Get-Process -Id $pidToWait -ErrorAction SilentlyContinue)) { break }; Start-Sleep -Milliseconds 500 }; if (Get-Process -Id $pidToWait -ErrorAction SilentlyContinue) { exit 1 }; $lock=[IO.File]::Open(` + psQuote(p.OperationLock()) + `,[IO.FileMode]::OpenOrCreate,[IO.FileAccess]::ReadWrite,[IO.FileShare]::ReadWrite); $locked=$false; try { for ($i=0; $i -lt 120; $i++) { try { $lock.Lock(0,1); $locked=$true; break } catch { Start-Sleep -Milliseconds 500 } }; if (-not $locked) { exit 1 }; $resolved=(Resolve-Path -LiteralPath $root).ProviderPath; if ($resolved -ne $root) { exit 1 }; if (([IO.File]::GetAttributes($root) -band [IO.FileAttributes]::ReparsePoint) -ne 0) { exit 1 }; if ([IO.File]::ReadAllText((Join-Path $root '` + markerName + `')) -ne ` + psQuote(markerContent) + `) { exit 1 }; if ([IO.File]::ReadAllText((Join-Path $root '.uninstalling')) -ne ` + psQuote(string(token)) + `) { exit 1 }; function Remove-OwnedDirectory([string]$directory) { foreach ($entry in [IO.Directory]::EnumerateFileSystemEntries($directory)) { $attributes=[IO.File]::GetAttributes($entry); if (($attributes -band [IO.FileAttributes]::Directory) -ne 0) { if (($attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { [IO.Directory]::Delete($entry,$false) } else { Remove-OwnedDirectory $entry } } else { [IO.File]::Delete($entry) } }; [IO.Directory]::Delete($directory,$false) }; Remove-OwnedDirectory $root } finally { if ($locked) { $lock.Unlock(0,1) }; $lock.Dispose() }`
	c := powershell(script)
	if e = c.Start(); e != nil {
		return fmt.Errorf("start deferred removal: %w", e)
	}
	return c.Process.Release()
}
