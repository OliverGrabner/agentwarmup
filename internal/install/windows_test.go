//go:build windows

package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDeferredInstalledExecutableRemoval(t *testing.T) {
	p := testPaths(t)
	if e := initialize(p); e != nil {
		t.Fatal(e)
	}
	src, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(src)
	if e != nil {
		t.Fatal(e)
	}
	dst := binary(p)
	if e = os.WriteFile(dst, data, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(p.Root, ".uninstalling"), []byte("\"isolated-test-generation\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	neighbor := filepath.Join(filepath.Dir(p.Root), "keep-neighbor")
	if e = os.WriteFile(neighbor, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	c := exec.Command(dst, "--test-owned-uninstall", p.Root)
	hide(c)
	if out, e := c.CombinedOutput(); e != nil {
		t.Fatalf("installed remover: %v: %s", e, out)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, e = os.Stat(p.Root)
		if os.IsNotExist(e) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deferred helper did not remove the installed executable and owned root")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, e = os.Stat(neighbor); e != nil {
		t.Fatal("deferred removal touched neighboring files")
	}
	if _, e = os.Stat(p.OperationLock()); e != nil {
		t.Fatal("external operation lock should remain for lifecycle serialization")
	}
}
