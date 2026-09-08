package job

import (
	"encoding/json"
	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/runner"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestScheduledGuardedFake runs the actual application through the native OS
// scheduler, using only the repository's offline fake-provider executable.
func TestScheduledGuardedFake(t *testing.T) {
	if os.Getenv("AGENTWARMUP_E2E") != "1" {
		t.Skip("opt-in native scheduler acceptance")
	}
	appExe, fakeExe := os.Getenv("AGENTWARMUP_E2E_EXE"), os.Getenv("AGENTWARMUP_E2E_PROVIDER")
	if !filepath.IsAbs(appExe) || !filepath.IsAbs(fakeExe) {
		t.Skip("set absolute AGENTWARMUP_E2E_EXE and AGENTWARMUP_E2E_PROVIDER built from this repository")
	}
	// Require the explicitly named offline fixture, never an installed CLI.
	if !strings.HasPrefix(filepath.Base(fakeExe), "fakeprovider") {
		t.Fatal("provider fixture must be named fakeprovider; never pass a real provider CLI")
	}
	p := config.Paths{Root: filepath.Join(t.TempDir(), "scheduled app 雪 & 50%")}
	if e := os.MkdirAll(filepath.Join(p.Root, "bin"), 0700); e != nil {
		t.Fatal(e)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	app := filepath.Join(p.Root, "bin", "agentwarmup"+ext)
	fake := filepath.Join(p.Root, "fixture", "fakeprovider"+ext)
	copyExe := func(from, to string) {
		t.Helper()
		b, e := os.ReadFile(from)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.MkdirAll(filepath.Dir(to), 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(to, b, 0700); e != nil {
			t.Fatal(e)
		}
	}
	copyExe(appExe, app)
	copyExe(fakeExe, fake)
	record := filepath.Join(p.Root, "fake-invocations.jsonl")
	if e := config.WriteJSON(filepath.Join(filepath.Dir(fake), "fake-options.json"), map[string]string{"mode": "ok", "record": record}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	trigger := now.Add(time.Minute).Truncate(time.Minute)
	reset := trigger.Add(5 * time.Hour)
	cfg := config.Config{SchemaVersion: 1, ResetTime: reset.Format("15:04"), ResetDays: []string{reset.Format("Mon")}, Timezone: "UTC", Enabled: true, EffectiveFrom: now, Providers: []config.ProviderConfig{{ID: "codex", Executable: fake, Version: "0.153.0", ConfigRoot: filepath.Join(p.Root, "fake-config"), AuthStore: "file"}}}
	if e := os.MkdirAll(cfg.Providers[0].ConfigRoot, 0700); e != nil {
		t.Fatal(e)
	}
	if e := config.Save(p.Config(), cfg); e != nil {
		t.Fatal(e)
	}
	m := New(p.Root)
	if e := m.Check(); e != nil {
		t.Fatal(e)
	}
	s, e := m.Inspect()
	if e != nil || s.Installed {
		t.Fatalf("unsafe test identity %+v: %v", s, e)
	}
	t.Cleanup(func() {
		if e := m.Uninstall(); e != nil {
			t.Errorf("remove isolated task %s: %v", m.Name, e)
		}
	})
	if e = m.Install(app); e != nil {
		t.Fatal(e)
	}
	deadline := trigger.Add(150 * time.Second)
	for {
		state, e := runner.LoadState(p.State("codex"), "codex")
		if e != nil {
			t.Fatal(e)
		}
		if state.Last != nil && state.Last.TriggerAt.Equal(trigger) && state.Last.Outcome != "attempted" {
			if state.Last.Outcome != "succeeded" {
				t.Fatalf("scheduled fake request: %+v", state.Last)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual guarded runner did not finish scheduled fake request")
		}
		time.Sleep(time.Second)
	}
	for i := 0; i < 2; i++ {
		if out, e := command(app, "check", "--home", p.Root); e != nil {
			t.Fatalf("duplicate check %s: %v", out, e)
		}
	}
	if out, e := command(app, "pause", "--home", p.Root); e != nil {
		t.Fatalf("pause %s: %v", out, e)
	}
	if out, e := command(app, "check", "--home", p.Root); e != nil {
		t.Fatalf("paused check %s: %v", out, e)
	}
	if out, e := command(app, "resume", "--home", p.Root); e != nil {
		t.Fatalf("resume %s: %v", out, e)
	}
	if out, e := command(app, "check", "--home", p.Root); e != nil {
		t.Fatalf("resumed check %s: %v", out, e)
	}
	doc, e := config.Load(p.Config())
	if e != nil || doc.Current == nil || !doc.Current.EffectiveFrom.After(trigger) {
		t.Fatalf("resume did not advance activation: %+v %v", doc, e)
	}
	data, e := os.ReadFile(record)
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d fake requests, want exactly one", len(lines))
	}
	var invocation struct {
		Args       []string `json:"args"`
		Dir        string   `json:"dir"`
		StdinBytes int      `json:"stdinBytes"`
	}
	if e = json.Unmarshal([]byte(lines[0]), &invocation); e != nil {
		t.Fatal(e)
	}
	if invocation.StdinBytes != 0 || !strings.Contains(strings.Join(invocation.Args, " "), "--ephemeral") {
		t.Fatalf("unexpected fake invocation %+v", invocation)
	}
	if rel, e := filepath.Rel(p.Root, invocation.Dir); e != nil || strings.HasPrefix(rel, "..") {
		t.Fatal("request escaped isolated root")
	}
	t.Logf("native scheduler invoked guarded runner once at %s; duplicate checks and pause sent no extra request", trigger.Format(time.RFC3339))
}
