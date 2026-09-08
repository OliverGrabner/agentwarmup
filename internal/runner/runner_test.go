package runner

import (
	"context"
	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/provider"
	"github.com/OliverGrabner/agentwarmup/internal/schedule"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testRunner(t *testing.T, ids ...string) (*Runner, time.Time) {
	t.Helper()
	paths := config.Paths{Root: t.TempDir()}
	now := time.Date(2026, 9, 8, 5, 0, 0, 0, time.UTC)
	c := config.Config{SchemaVersion: 1, ResetTime: "10:00", ResetDays: []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}, Timezone: "UTC", Enabled: true, EffectiveFrom: now.Add(-time.Hour)}
	for _, id := range ids {
		c.Providers = append(c.Providers, config.ProviderConfig{ID: id, Executable: filepath.Join(paths.Root, id+".exe"), Version: "99.0.0"})
	}
	if err := config.Save(paths.Config(), c); err != nil {
		t.Fatal(err)
	}
	r := New(paths)
	r.Now = func() time.Time { return now }
	r.Prepare = func(_ context.Context, p config.ProviderConfig, _ string) (provider.Invocation, provider.Result) {
		return provider.Invocation{Executable: p.ID}, provider.Result{}
	}
	r.Execute = func(context.Context, provider.Invocation) provider.Result {
		return provider.Result{Outcome: "succeeded"}
	}
	return r, now
}
func testOccurrence(t *testing.T, now time.Time, id string) schedule.Occurrence {
	t.Helper()
	s, err := schedule.New("10:00", []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}, "UTC")
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.Latest(now, id, 5*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return o
}
func TestConcurrentChecksIndependentProviders(t *testing.T) {
	r, _ := testRunner(t, "codex", "claude")
	entered := make(chan string, 2)
	release := make(chan struct{})
	var count atomic.Int32
	r.Execute = func(_ context.Context, inv provider.Invocation) provider.Result {
		count.Add(1)
		state, err := LoadState(r.Paths.State(inv.Executable), inv.Executable)
		if err != nil || state.Last == nil || state.Last.Outcome != "attempted" {
			t.Errorf("attempt not persisted before execute: %+v %v", state, err)
		}
		entered <- inv.Executable
		<-release
		if inv.Executable == "codex" {
			return provider.Result{Outcome: "failed"}
		}
		return provider.Result{Outcome: "succeeded"}
	}
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); errors <- r.Check(context.Background()) }()
	}
	seen := map[string]bool{}
	for n := 0; n < 2; n++ {
		select {
		case id := <-entered:
			seen[id] = true
		case <-time.After(3 * time.Second):
			close(release)
			wg.Wait()
			t.Fatal("providers did not execute independently")
		}
	}
	close(release)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
	if len(seen) != 2 || count.Load() != 2 {
		t.Fatalf("seen %v, calls %d", seen, count.Load())
	}
	if err := r.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal("terminal outcomes retried")
	}
}
func TestAttemptedMarkerNeverRetries(t *testing.T) {
	r, now := testRunner(t, "codex")
	o := testOccurrence(t, now, "codex")
	s := State{SchemaVersion: 1, Records: []Record{}}
	if err := s.save(r.Paths.State("codex"), Record{Key: o.Key, ResetAt: o.ResetAt, TriggerAt: o.TriggerAt, AttemptedAt: now, Outcome: "attempted"}, now); err != nil {
		t.Fatal(err)
	}
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		t.Error("prepared existing attempt")
		return provider.Invocation{}, provider.Result{}
	}
	for n := 0; n < 2; n++ {
		if err := r.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	s, err := LoadState(r.Paths.State("codex"), "codex")
	if err != nil || s.Last.Outcome != "interrupted" {
		t.Fatalf("state %+v %v", s, err)
	}
}
func TestMissed121SecondsRecordedOnce(t *testing.T) {
	r, now := testRunner(t, "codex")
	r.Now = func() time.Time { return now.Add(121 * time.Second) }
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		t.Error("late provider prepared")
		return provider.Invocation{}, provider.Result{}
	}
	for n := 0; n < 2; n++ {
		if err := r.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	s, err := LoadState(r.Paths.State("codex"), "codex")
	if err != nil || len(s.Records) != 1 || s.Last.Outcome != "missed" {
		t.Fatalf("state %+v %v", s, err)
	}
}
func TestCorruptStateFailsClosed(t *testing.T) {
	r, _ := testRunner(t, "codex")
	if err := os.WriteFile(r.Paths.State("codex"), []byte(`{"schemaVersion":`), 0600); err != nil {
		t.Fatal(err)
	}
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		t.Error("prepared with corrupt state")
		return provider.Invocation{}, provider.Result{}
	}
	if err := r.Check(context.Background()); err == nil {
		t.Fatal("corrupt state accepted")
	}
}
func TestUnwritableAttemptFailsClosed(t *testing.T) {
	r, _ := testRunner(t, "codex")
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		if err := os.Mkdir(r.Paths.State("codex"), 0700); err != nil {
			t.Error(err)
		}
		return provider.Invocation{}, provider.Result{}
	}
	r.Execute = func(context.Context, provider.Invocation) provider.Result {
		t.Error("launched without durable state")
		return provider.Result{Outcome: "succeeded"}
	}
	if err := r.Check(context.Background()); err == nil {
		t.Fatal("failed state write accepted")
	}
}
func TestPreparationCrossingDeadlineSkips(t *testing.T) {
	r, now := testRunner(t, "codex")
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	r.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		clock.Store(now.Add(121 * time.Second).UnixNano())
		return provider.Invocation{}, provider.Result{}
	}
	r.Execute = func(context.Context, provider.Invocation) provider.Result {
		t.Error("launched after admission deadline")
		return provider.Result{Outcome: "succeeded"}
	}
	if err := r.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState(r.Paths.State("codex"), "codex")
	if err != nil || s.Last.Outcome != "missed" {
		t.Fatalf("state %+v %v", s, err)
	}
}
func TestActivationExcludesEarlierTrigger(t *testing.T) {
	r, now := testRunner(t, "codex")
	d, err := config.Load(r.Paths.Config())
	if err != nil {
		t.Fatal(err)
	}
	d.Current.EffectiveFrom = now.Add(time.Nanosecond)
	if err = config.Save(r.Paths.Config(), *d.Current); err != nil {
		t.Fatal(err)
	}
	r.Now = func() time.Time { return now.Add(time.Second) }
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		t.Error("prepared pre-activation occurrence")
		return provider.Invocation{}, provider.Result{}
	}
	if err = r.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(r.Paths.State("codex")); !os.IsNotExist(err) {
		t.Fatal("pre-activation state written")
	}
}

func TestPauseDuringPreparationPreventsAdmission(t *testing.T) {
	r, _ := testRunner(t, "codex")
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		err := WithLock(context.Background(), r.Paths.Lock("lifecycle"), func() error {
			d, err := config.Load(r.Paths.Config())
			if err != nil {
				return err
			}
			d.Current.Enabled = false
			return config.Save(r.Paths.Config(), *d.Current)
		})
		if err != nil {
			t.Error(err)
		}
		return provider.Invocation{}, provider.Result{}
	}
	r.Execute = func(context.Context, provider.Invocation) provider.Result {
		t.Error("launched after pause")
		return provider.Result{Outcome: "succeeded"}
	}
	if err := r.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Paths.State("codex")); !os.IsNotExist(err) {
		t.Fatal("paused occurrence marked attempted")
	}
}

func TestPreparationFailureIsTerminal(t *testing.T) {
	r, _ := testRunner(t, "codex")
	var probes atomic.Int32
	r.Prepare = func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result) {
		probes.Add(1)
		return provider.Invocation{}, provider.Result{Outcome: "auth_required", Detail: "Sign in through setup."}
	}
	r.Execute = func(context.Context, provider.Invocation) provider.Result {
		t.Error("launched with failed authentication prerequisite")
		return provider.Result{Outcome: "succeeded"}
	}
	for n := 0; n < 2; n++ {
		if err := r.Check(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if probes.Load() != 1 {
		t.Fatal("repeated failed prerequisite for same occurrence")
	}
	s, err := LoadState(r.Paths.State("codex"), "codex")
	if err != nil || s.Last.Outcome != "auth_required" {
		t.Fatalf("state %+v %v", s, err)
	}
}
