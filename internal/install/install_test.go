package install

import (
	"bytes"
	"fmt"
	"github.com/OliverGrabner/agentwarmup/internal/config"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "--test-owned-uninstall" {
		p := config.Paths{Root: os.Args[2]}
		if e := Uninstall(p); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("agentwarmup 1.2.3")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func testPaths(t *testing.T) config.Paths {
	t.Helper()
	return config.Paths{Root: filepath.Join(t.TempDir(), "owned app 雪 & 50%")}
}
func TestOwnedRootSafety(t *testing.T) {
	p := testPaths(t)
	if Uninstall(p) == nil {
		t.Fatal("removed unowned root")
	}
	h, _ := os.UserHomeDir()
	for _, bad := range []string{".", h, filepath.VolumeName(p.Root) + string(os.PathSeparator)} {
		if validateRoot(bad) == nil {
			t.Fatalf("accepted dangerous root %q", bad)
		}
	}
	if e := os.MkdirAll(p.Root, 0700); e != nil {
		t.Fatal(e)
	}
	unrelated := filepath.Join(p.Root, "notes.txt")
	if e := os.WriteFile(unrelated, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if initialize(p) == nil {
		t.Fatal("claimed unrelated directory")
	}
	if _, e := os.Stat(unrelated); e != nil {
		t.Fatal(e)
	}
}

func TestRollbackBeforeOwnershipLeavesUnrelatedFiles(t *testing.T) {
	p := testPaths(t)
	if e := os.MkdirAll(p.Root, 0700); e != nil {
		t.Fatal(e)
	}
	sentinel := filepath.Join(p.Root, "unrelated.txt")
	want := []byte("leave this destination unchanged")
	if e := os.WriteFile(sentinel, want, 0600); e != nil {
		t.Fatal(e)
	}
	if e := initialize(p); e == nil {
		t.Fatal("claimed unrelated destination")
	}
	if e := Rollback(p); e != nil {
		t.Fatalf("rollback before initialization: %v", e)
	}
	got, e := os.ReadFile(sentinel)
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("rollback changed unrelated file")
	}
	entries, e := os.ReadDir(p.Root)
	if e != nil || len(entries) != 1 {
		t.Fatal("rollback changed unowned directory contents")
	}
	if e := os.MkdirAll(filepath.Dir(backup(p)), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(backup(p), want, 0600); e != nil {
		t.Fatal(e)
	}
	if e := Rollback(p); e == nil {
		t.Fatal("restored an unowned backup")
	}
	got, e = os.ReadFile(backup(p))
	if e != nil || !bytes.Equal(got, want) {
		t.Fatal("rollback modified unowned backup")
	}
	if _, e := os.Lstat(binary(p)); !os.IsNotExist(e) {
		t.Fatal("rollback created an unowned executable")
	}
}
func TestEnsureUpdateRollbackAndPreservation(t *testing.T) {
	p := testPaths(t)
	src, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	dst, e := ensureFrom(p, src)
	if e != nil {
		t.Fatal(e)
	}
	first, e := os.ReadFile(dst)
	if e != nil {
		t.Fatal(e)
	}
	configFile := filepath.Join(p.Root, "config.json")
	sentinel := []byte(`{"effectiveFrom":"unchanged","enabled":false}`)
	if e = os.WriteFile(configFile, sentinel, 0600); e != nil {
		t.Fatal(e)
	}
	modified := filepath.Join(t.TempDir(), "new-agentwarmup")
	if runtime.GOOS == "windows" {
		modified += ".exe"
	}
	second := append(append([]byte{}, first...), []byte("test-build-padding")...)
	if e = os.WriteFile(modified, second, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = ensureFrom(p, modified); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(backup(p))
	if e != nil || !bytes.Equal(b, first) {
		t.Fatal("previous executable not retained")
	}
	if _, e = ensureFrom(p, src); e == nil {
		t.Fatal("overwrote interrupted update backup")
	}
	if e = Rollback(p); e != nil {
		t.Fatal(e)
	}
	b, e = os.ReadFile(dst)
	if e != nil || !bytes.Equal(b, first) {
		t.Fatal("rollback did not restore previous executable")
	}
	b, e = os.ReadFile(configFile)
	if e != nil || !bytes.Equal(b, sentinel) {
		t.Fatal("configuration changed")
	}
	if _, e = ensureFrom(p, modified); e != nil {
		t.Fatal(e)
	}
	// Test binary commit without editing the machine's PATH. Platform-specific
	// PATH tests operate on isolated profiles or inspect generated commands.
	if e = finishCommit(p); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(backup(p)); !os.IsNotExist(e) {
		t.Fatal("committed backup remains")
	}
	b, _ = os.ReadFile(dst)
	if !bytes.Equal(b, second) {
		t.Fatal("commit lost new executable")
	}
}
func TestBadExecutableLeavesInstalledBinary(t *testing.T) {
	p := testPaths(t)
	src, _ := os.Executable()
	dst, e := ensureFrom(p, src)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(dst)
	bad := filepath.Join(t.TempDir(), "broken.exe")
	if e = os.WriteFile(bad, []byte("not executable"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = ensureFrom(p, bad); e == nil {
		t.Fatal("invalid executable accepted")
	}
	after, _ := os.ReadFile(dst)
	if !bytes.Equal(before, after) {
		t.Fatal("failed verification modified installed binary")
	}
}
func TestDowngrade(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"agentwarmup 1.2.3", "agentwarmup 1.2.2", true}, {"agentwarmup v2.0.0", "agentwarmup v1.9.9", true}, {"agentwarmup 1.2.3", "agentwarmup 1.2.3-rc.1", true}, {"agentwarmup 1.2.3-rc.1", "agentwarmup 1.2.3", false}, {"agentwarmup dev", "agentwarmup 1.2.3", false}} {
		if got := downgrade(c.a, c.b); got != c.want {
			t.Errorf("%s -> %s: %v", c.a, c.b, got)
		}
	}
}
func TestUninstallOnlyOwnedRoot(t *testing.T) {
	p := testPaths(t)
	if e := initialize(p); e != nil {
		t.Fatal(e)
	}
	neighbor := filepath.Join(filepath.Dir(p.Root), "neighbor")
	if e := os.WriteFile(neighbor, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := Uninstall(p); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(p.Root); !os.IsNotExist(e) {
		t.Fatal("owned root remains")
	}
	if _, e := os.Stat(neighbor); e != nil {
		t.Fatal("neighbor was removed")
	}
}
