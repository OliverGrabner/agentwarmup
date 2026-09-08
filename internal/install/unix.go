//go:build !windows

package install

import (
	"fmt"
	"github.com/OliverGrabner/agentwarmup/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func hide(c *exec.Cmd)              {}
func replace(from, to string) error { return os.Rename(from, to) }
func shellQuote(s string) string    { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func profilePath(home, shell string) string {
	switch filepath.Base(shell) {
	case "bash":
		return filepath.Join(home, ".bashrc")
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		root := os.Getenv("XDG_CONFIG_HOME")
		if !filepath.IsAbs(root) {
			root = filepath.Join(home, ".config")
		}
		return filepath.Join(root, "fish", "config.fish")
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, ".zprofile")
	}
	return filepath.Join(home, ".profile")
}
func profileBlock(p config.Paths, profile string) string {
	bin := filepath.Dir(binary(p))
	statement := "export PATH=" + shellQuote(bin) + ":\"$PATH\""
	if filepath.Ext(profile) == ".fish" {
		q := "'" + strings.ReplaceAll(strings.ReplaceAll(bin, "\\", "\\\\"), "'", "\\'") + "'"
		statement = "set -gx PATH " + q + " $PATH"
	}
	return "\n# >>> AgentWarmup " + p.Root + " >>>\n" + statement + "\n# <<< AgentWarmup " + p.Root + " <<<\n"
}
func addPath(p config.Paths) error {
	if r, e := readPathRecord(p); e != nil {
		return e
	} else if r.Kind != "" {
		return repairPath(p, r)
	}
	bin := filepath.Dir(binary(p))
	for _, part := range filepath.SplitList(os.Getenv("PATH")) {
		if samePath(part, bin) {
			return nil
		}
	}
	h, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	link := filepath.Join(h, ".local", "bin", "agentwarmup")
	for _, part := range filepath.SplitList(os.Getenv("PATH")) {
		if part != filepath.Dir(link) {
			continue
		}
		if target, e := os.Readlink(link); e == nil && target == binary(p) {
			return nil
		}
		if _, e := os.Lstat(link); os.IsNotExist(e) {
			if e = savePathRecord(p, pathRecord{Kind: "link", Path: link, Block: binary(p)}); e != nil {
				return e
			}
			return os.Symlink(binary(p), link)
		}
	}
	profile := profilePath(h, os.Getenv("SHELL"))
	if e = os.MkdirAll(filepath.Dir(profile), 0700); e != nil {
		return e
	}
	mode := os.FileMode(0600)
	if st, e := os.Stat(profile); e == nil {
		mode = st.Mode().Perm()
	}
	if shell := filepath.Base(os.Getenv("SHELL")); shell != "bash" && shell != "zsh" && shell != "fish" && shell != "sh" && shell != "dash" && shell != "." {
		return fmt.Errorf("shell %s needs manual PATH setup; add %s then rerun setup", shell, bin)
	}
	if st, e := os.Lstat(profile); e == nil && st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("shell profile is a symbolic link; add %s to PATH manually", bin)
	}
	data, e := os.ReadFile(profile)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	block := profileBlock(p, profile)
	// Record before mutation so interrupted installs can remove their block.
	if e = savePathRecord(p, pathRecord{Kind: "profile", Path: profile, Block: block}); e != nil {
		return e
	}
	return atomicWrite(profile, append(data, []byte(block)...), mode)
}
func allowedProfile(home, path string) bool {
	return path == filepath.Join(home, ".profile") || path == filepath.Join(home, ".zprofile") || path == filepath.Join(home, ".bashrc") || path == filepath.Join(home, ".zshrc") || path == profilePath(home, "fish")
}

// Ownership is recorded before the mutation; recovery verifies and completes
// that mutation instead of treating the record itself as proof of completion.
func repairPath(p config.Paths, r pathRecord) error {
	h, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	if r.Kind == "link" {
		if r.Path != filepath.Join(h, ".local", "bin", "agentwarmup") || r.Block != binary(p) {
			return fmt.Errorf("invalid owned command link")
		}
		target, e := os.Readlink(r.Path)
		if e == nil {
			if target != binary(p) {
				return fmt.Errorf("owned command link was changed")
			}
			return nil
		}
		if !os.IsNotExist(e) {
			return e
		}
		return os.Symlink(binary(p), r.Path)
	}
	if r.Kind != "profile" || !allowedProfile(h, r.Path) || r.Block != profileBlock(p, r.Path) {
		return fmt.Errorf("invalid owned profile record")
	}
	if st, e := os.Lstat(r.Path); e == nil && st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("owned shell profile became a symbolic link")
	}
	data, e := os.ReadFile(r.Path)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if strings.Contains(string(data), r.Block) {
		return nil
	}
	mode := os.FileMode(0600)
	if st, e := os.Stat(r.Path); e == nil {
		mode = st.Mode().Perm()
	}
	if e = os.MkdirAll(filepath.Dir(r.Path), 0700); e != nil {
		return e
	}
	return atomicWrite(r.Path, append(data, []byte(r.Block)...), mode)
}
func removePath(p config.Paths) error {
	r, e := readPathRecord(p)
	if e != nil || r.Kind == "" {
		return e
	}
	h, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	if r.Kind == "link" {
		if r.Path != filepath.Join(h, ".local", "bin", "agentwarmup") || r.Block != binary(p) {
			return fmt.Errorf("invalid owned command link")
		}
		target, e := os.Readlink(r.Path)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if target != binary(p) {
			return nil
		}
		return os.Remove(r.Path)
	}
	if r.Kind != "profile" || !allowedProfile(h, r.Path) {
		return fmt.Errorf("invalid owned PATH record")
	}
	expected := profileBlock(p, r.Path)
	if r.Block != expected {
		return fmt.Errorf("invalid owned profile block")
	}
	data, e := os.ReadFile(r.Path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !strings.Contains(string(data), r.Block) {
		return nil
	}
	st, e := os.Stat(r.Path)
	if e != nil {
		return e
	}
	return atomicWrite(r.Path, []byte(strings.Replace(string(data), r.Block, "", 1)), st.Mode().Perm())
}
func removeRoot(p config.Paths) error {
	if e := owned(p); e != nil {
		return e
	}
	return os.RemoveAll(p.Root)
}
