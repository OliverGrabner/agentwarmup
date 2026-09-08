// Package job registers one per-user, minute-interval guarded check.
package job

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type Manager struct{ Root, Name string }
type Status struct {
	Installed, Enabled, Loaded bool
	Detail                     string
}

// New derives the job identity from the absolute installation root. Separate
// roots (including tests) cannot replace the ordinary user's job.
func New(root string) Manager {
	abs, err := filepath.Abs(root)
	if err == nil {
		root = filepath.Clean(abs)
	}
	identity := root
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
	}
	sum := sha256.Sum256([]byte(identity))
	return Manager{Root: root, Name: fmt.Sprintf("agentwarmup-%x", sum[:8])}
}

var validName = regexp.MustCompile(`^agentwarmup-[a-zA-Z0-9-]{1,80}$`)

func (m Manager) validate() error {
	if !filepath.IsAbs(m.Root) || filepath.Clean(m.Root) == filepath.VolumeName(m.Root)+string(os.PathSeparator) {
		return fmt.Errorf("job root must be an absolute application directory")
	}
	if strings.ContainsAny(m.Root, "\x00\r\n") || !validName.MatchString(m.Name) {
		return fmt.Errorf("invalid job root or name")
	}
	return nil
}
func (m Manager) validateExe(exe string) error {
	if err := m.validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(exe) || strings.ContainsAny(exe, "\x00\r\n") {
		return fmt.Errorf("job executable must be an absolute path")
	}
	st, err := os.Stat(exe)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("job executable is not a regular file")
	}
	return nil
}
func escapeXML(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func command(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	hide(c)
	out, err := c.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", filepath.Base(name), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".agentwarmup-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return replaceFile(f.Name(), path)
}
