//go:build !windows

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnedShellProfileAndRemoval(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			h := t.TempDir()
			t.Setenv("HOME", h)
			t.Setenv("SHELL", "/bin/"+shell)
			t.Setenv("PATH", "/usr/bin:/bin")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(h, ".config"))
			p := testPaths(t)
			if e := initialize(p); e != nil {
				t.Fatal(e)
			}
			profile := profilePath(h, "/bin/"+shell)
			if e := os.MkdirAll(filepath.Dir(profile), 0700); e != nil {
				t.Fatal(e)
			}
			original := "# keep custom content\n"
			if e := os.WriteFile(profile, []byte(original), 0640); e != nil {
				t.Fatal(e)
			}
			if e := addPath(p); e != nil {
				t.Fatal(e)
			}
			data, _ := os.ReadFile(profile)
			if !strings.HasPrefix(string(data), original) {
				t.Fatal("profile content changed")
			}
			if shell == "fish" && !strings.Contains(string(data), "set -gx PATH") {
				t.Fatal("fish syntax missing")
			}
			if e := removePath(p); e != nil {
				t.Fatal(e)
			}
			data, _ = os.ReadFile(profile)
			if string(data) != original {
				t.Fatal("profile content not restored")
			}
			st, _ := os.Stat(profile)
			if st.Mode().Perm() != 0640 {
				t.Fatal("profile permissions changed")
			}
		})
	}
}
func TestOwnedCommandLink(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	dir := filepath.Join(h, ".local", "bin")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", dir+":/bin")
	p := testPaths(t)
	if e := initialize(p); e != nil {
		t.Fatal(e)
	}
	if e := addPath(p); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "agentwarmup")
	if target, e := os.Readlink(link); e != nil || target != binary(p) {
		t.Fatalf("%s %v", target, e)
	}
	if e := removePath(p); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Lstat(link); !os.IsNotExist(e) {
		t.Fatal("owned link remains")
	}
}
