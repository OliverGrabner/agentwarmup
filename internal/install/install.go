// Package install manages only AgentWarmup-owned files. Callers serialize
// lifecycle changes and quiesce admissions before replacing the executable.
package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/config"
)

const markerName = ".agentwarmup-owned"
const markerContent = "AgentWarmup installation schema 1\n"

func binary(p config.Paths) string {
	n := "agentwarmup"
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return filepath.Join(p.Root, "bin", n)
}
func backup(p config.Paths) string { return filepath.Join(p.Root, "bin", ".agentwarmup.previous") }

func validateRoot(root string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.ContainsAny(root, "\x00\r\n") {
		return fmt.Errorf("installation root must be a clean absolute directory")
	}
	volume := filepath.VolumeName(root) + string(os.PathSeparator)
	home, _ := os.UserHomeDir()
	for _, forbidden := range []string{volume, home, os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"), os.Getenv("XDG_CONFIG_HOME"), os.TempDir()} {
		if forbidden != "" && samePath(root, filepath.Clean(forbidden)) {
			return fmt.Errorf("refusing to use a general-purpose directory as the application root")
		}
	}
	for p := root; ; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e == nil && s.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("installation root cannot traverse a symbolic link: %s", p)
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
func owned(p config.Paths) error {
	if e := validateRoot(p.Root); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(p.Root, markerName))
	if e != nil {
		return fmt.Errorf("cannot verify application ownership: %w", e)
	}
	if string(b) != markerContent {
		return fmt.Errorf("invalid application ownership marker")
	}
	return nil
}
func initialize(p config.Paths) error {
	if e := validateRoot(p.Root); e != nil {
		return e
	}
	if _, e := os.Stat(filepath.Join(p.Root, markerName)); e == nil {
		return owned(p)
	} else if !os.IsNotExist(e) {
		return e
	}
	entries, e := os.ReadDir(p.Root)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	allowed := map[string]bool{"bin": true, "config.json": true, "locks": true, "state": true, "run": true, "runs": true, "lastrun.json": true, "log.txt": true, "config.json.previous": true, "codex-state.json": true, "claude-state.json": true, "lifecycle.json": true, ".uninstalling": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return fmt.Errorf("destination contains unrelated files; choose a dedicated AgentWarmup directory")
		}
	}
	if e = os.MkdirAll(filepath.Join(p.Root, "bin"), 0700); e != nil {
		return e
	}
	return atomicWrite(filepath.Join(p.Root, markerName), []byte(markerContent), 0600)
}
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".agentwarmup-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(data)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return replace(f.Name(), path)
}
func version(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, path, "--version")
	hide(c)
	out, e := c.Output()
	if e != nil {
		return "", fmt.Errorf("verify executable version: %w", e)
	}
	v := strings.TrimSpace(string(out))
	if v == "" || len(v) > 200 {
		return "", fmt.Errorf("invalid executable version response")
	}
	return v, nil
}

var semver = regexp.MustCompile(`(?:^|\s)v?(\d+)\.(\d+)\.(\d+)([^\s]*)`)

func downgrade(old, new string) bool {
	a, b := semver.FindStringSubmatch(old), semver.FindStringSubmatch(new)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(a[i])
		y, _ := strconv.Atoi(b[i])
		if x != y {
			return y < x
		}
	}
	return a[4] == "" && b[4] != ""
}

// Ensure stages and verifies this executable, then retains the previous binary
// until Commit. Rollback restores it if job registration/config commit fails.
func Ensure(p config.Paths) (string, error) {
	src, e := os.Executable()
	if e != nil {
		return "", e
	}
	return ensureFrom(p, src)
}
func ensureFrom(p config.Paths, src string) (string, error) {
	if e := initialize(p); e != nil {
		return "", e
	}
	dst := binary(p)
	if samePath(filepath.Clean(src), dst) {
		return dst, nil
	}
	data, e := os.ReadFile(src)
	if e != nil {
		return "", e
	}
	if old, e := os.ReadFile(dst); e == nil && sha256.Sum256(old) == sha256.Sum256(data) {
		return dst, nil
	}
	// An uncommitted prior update must be resolved before starting another.
	if _, e = os.Stat(backup(p)); e == nil {
		return "", fmt.Errorf("an interrupted update needs repair before replacing the executable")
	} else if !os.IsNotExist(e) {
		return "", e
	}
	f, e := os.CreateTemp(filepath.Dir(dst), ".agentwarmup-stage-*")
	if e != nil {
		return "", e
	}
	stage := f.Name()
	if runtime.GOOS == "windows" {
		f.Close()
		os.Remove(stage)
		stage += ".exe"
		f, e = os.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if e != nil {
			return "", e
		}
	}
	defer os.Remove(stage)
	if e = f.Chmod(0700); e == nil {
		_, e = io.Copy(f, bytes.NewReader(data))
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return "", e
	}
	v, e := version(stage)
	if e != nil {
		return "", e
	}
	hadOld := false
	if _, e = os.Stat(dst); e == nil {
		oldV, err := version(dst)
		if err != nil {
			return "", err
		}
		if downgrade(oldV, v) {
			return "", fmt.Errorf("refusing automatic downgrade from %s to %s", oldV, v)
		}
		if e = replace(dst, backup(p)); e != nil {
			return "", fmt.Errorf("quiesce the installed process before updating: %w", e)
		}
		hadOld = true
	} else if !os.IsNotExist(e) {
		return "", e
	}
	if e = replace(stage, dst); e != nil {
		if hadOld {
			e = errors.Join(e, replace(backup(p), dst))
		}
		return "", e
	}
	return dst, nil
}
func Commit(p config.Paths) error {
	if e := owned(p); e != nil {
		return e
	}
	if e := addPath(p); e != nil {
		return e
	}
	return finishCommit(p)
}
func finishCommit(p config.Paths) error {
	e := os.Remove(backup(p))
	if os.IsNotExist(e) {
		return nil
	}
	return e
}
func Rollback(p config.Paths) error {
	if e := validateRoot(p.Root); e != nil {
		return e
	}
	// Setup can journal its configuration before initialization discovers an
	// unsuitable destination. No ownership marker and no backup means this
	// transaction never replaced a binary, so there is nothing to restore.
	if _, e := os.Lstat(filepath.Join(p.Root, markerName)); os.IsNotExist(e) {
		if _, backupErr := os.Lstat(backup(p)); os.IsNotExist(backupErr) {
			return nil
		} else if backupErr != nil {
			return backupErr
		}
		return fmt.Errorf("refusing to restore an executable backup without verified application ownership")
	} else if e != nil {
		return e
	}
	if e := owned(p); e != nil {
		return e
	}
	if _, e := os.Stat(backup(p)); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	return replace(backup(p), binary(p))
}

// Uninstall must follow successful job removal and release of lifecycle locks.
func Uninstall(p config.Paths) error {
	if e := owned(p); e != nil {
		return e
	}
	if e := removePath(p); e != nil {
		return e
	}
	return removeRoot(p)
}

type pathRecord struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Block string `json:"block,omitempty"`
}

func readPathRecord(p config.Paths) (pathRecord, error) {
	b, e := os.ReadFile(filepath.Join(p.Root, "path.json"))
	if os.IsNotExist(e) {
		return pathRecord{}, nil
	}
	if e != nil {
		return pathRecord{}, e
	}
	var r pathRecord
	e = json.Unmarshal(b, &r)
	return r, e
}
func savePathRecord(p config.Paths, r pathRecord) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return atomicWrite(filepath.Join(p.Root, "path.json"), b, 0600)
}
