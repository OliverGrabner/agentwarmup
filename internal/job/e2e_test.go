package job

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "check" && os.Args[2] == "--home" {
		root := os.Args[3]
		if filepath.IsAbs(root) && filepath.Base(root) == "isolated-scheduler" {
			e := os.WriteFile(filepath.Join(root, "scheduler-fired"), []byte("fake guarded check only\n"), 0600)
			if e != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
		os.Exit(2)
	}
	os.Exit(m.Run())
}
func TestIsolatedSchedulerLifecycle(t *testing.T) {
	if os.Getenv("AGENTWARMUP_E2E") != "1" {
		t.Skip("set AGENTWARMUP_E2E=1 for isolated native scheduler acceptance")
	}
	root := filepath.Join(t.TempDir(), "isolated-scheduler")
	if e := os.Mkdir(root, 0700); e != nil {
		t.Fatal(e)
	}
	m := New(root)
	m.Name = fmt.Sprintf("agentwarmup-test-%d-%d", os.Getpid(), time.Now().UnixNano())
	if e := m.Check(); e != nil {
		t.Fatal(e)
	}
	s, e := m.Inspect()
	if e != nil {
		t.Fatal(e)
	}
	if s.Installed {
		t.Fatal("test identity unexpectedly exists")
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := m.Uninstall(); e != nil {
			t.Errorf("cleanup %s: %v", m.Name, e)
		}
	})
	if e = m.Install(exe); e != nil {
		t.Fatal(e)
	}
	s, e = m.Inspect()
	if e != nil || !s.Installed || !s.Enabled {
		t.Fatalf("installed status %+v: %v", s, e)
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		if _, e = os.Stat(filepath.Join(root, "scheduler-fired")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not launch isolated fake check within 90 seconds")
		}
		time.Sleep(time.Second)
	}
	if e = m.SetEnabled(false); e != nil {
		t.Fatal(e)
	}
	s, e = m.Inspect()
	if e != nil || !s.Installed || s.Enabled {
		t.Fatalf("disabled status %+v: %v", s, e)
	}
	if e = m.SetEnabled(true); e != nil {
		t.Fatal(e)
	}
	if e = m.Uninstall(); e != nil {
		t.Fatal(e)
	}
	s, e = m.Inspect()
	if e != nil || s.Installed {
		t.Fatalf("removed status %+v: %v", s, e)
	}
	if e = m.Uninstall(); e != nil {
		t.Fatal(e)
	}
}
