package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/job"
	"github.com/OliverGrabner/agentwarmup/internal/provider"
)

type fakeJobs struct {
	status                  job.Status
	installs, removes       int
	failInstall, failRemove bool
	duringInstall           func()
	executable              string
}

func (*fakeJobs) Check() error { return nil }
func (j *fakeJobs) Install(executable string) error {
	j.executable = executable
	j.installs++
	if j.duringInstall != nil {
		j.duringInstall()
	}
	if j.failInstall {
		j.failInstall = false
		return errors.New("simulated registration failure")
	}
	j.status = job.Status{Installed: true, Enabled: true}
	return nil
}
func (j *fakeJobs) Inspect() (job.Status, error) { return j.status, nil }
func (j *fakeJobs) SetEnabled(on bool) error     { j.status.Enabled = on; return nil }
func (j *fakeJobs) Uninstall() error {
	j.removes++
	if j.failRemove {
		return errors.New("simulated removal failure")
	}
	j.status = job.Status{}
	return nil
}

func testApp(t *testing.T, answers string) (*app, *fakeJobs, *bytes.Buffer) {
	t.Helper()
	paths := config.Paths{Root: t.TempDir()}
	a := newApp(paths)
	j := &fakeJobs{}
	out := &bytes.Buffer{}
	a.in, a.out, a.jobs = bufio.NewReader(strings.NewReader(answers)), out, j
	a.now = func() time.Time { return time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC) }
	a.zone = func() (string, error) { return "America/Chicago", nil }
	a.detect = func(_ context.Context, id string) (provider.Installation, error) {
		return provider.Installation{ID: id, Executable: filepath.Join(paths.Root, id), Version: "99.0.0"}, nil
	}
	a.auth = func(context.Context, provider.Installation) (provider.AuthStatus, error) {
		return provider.AuthStatus{Kind: "subscription"}, nil
	}
	a.verify = func(_ context.Context, i provider.Installation) (provider.Installation, error) { return i, nil }
	a.login = func(context.Context, provider.Installation) error { t.Fatal("unexpected login"); return nil }
	a.ensure = func(config.Paths) (string, error) { return paths.Bin(), nil }
	a.commit, a.rollback = func(config.Paths) error { return nil }, func(config.Paths) error { return nil }
	a.removeFiles = func(config.Paths) error { return nil }
	return a, j, out
}

func saved(t *testing.T, a *app) config.Config {
	t.Helper()
	d, e := config.Load(a.paths.Config())
	if e != nil {
		t.Fatal(e)
	}
	return *d.Current
}
func seed(t *testing.T, a *app, enabled bool) config.Config {
	t.Helper()
	c := config.Config{SchemaVersion: 1, ResetTime: "02:00", ResetDays: []string{"Mon", "Wed", "Fri"}, Timezone: "America/Chicago", Enabled: enabled,
		EffectiveFrom: a.now().Add(-time.Hour).UTC(), Providers: []config.ProviderConfig{{ID: "codex", Executable: filepath.Join(a.paths.Root, "codex"), Version: "99.0.0"}}}
	if e := config.Save(a.paths.Config(), c); e != nil {
		t.Fatal(e)
	}
	return c
}

func TestSetupBothProvidersAndNoAdmissionDuringInstall(t *testing.T) {
	a, j, out := testApp(t, "\n\n\n\n")
	j.duringInstall = func() {
		if saved(t, a).Enabled {
			t.Fatal("enabled before job verification")
		}
	}
	if err := a.setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := saved(t, a)
	if len(c.Providers) != 2 || c.ResetTime != "10:00" || !c.Enabled || !c.EffectiveFrom.Equal(a.now()) {
		t.Fatalf("%+v", c)
	}
	if j.installs != 1 || !strings.Contains(out.String(), "No test request was sent") || !strings.Contains(out.String(), "Target reset:") {
		t.Fatal(out.String())
	}
	if j.executable != a.paths.Bin() {
		t.Fatalf("scheduled %q instead of installed executable %q", j.executable, a.paths.Bin())
	}
}

func TestSetupEOFNeverAcceptsDefaults(t *testing.T) {
	for _, answers := range []string{"", "\n", "\n\n", "\n\n\n", "\n\n\ny"} {
		t.Run(strings.ReplaceAll(answers, "\n", "enter"), func(t *testing.T) {
			a, j, _ := testApp(t, answers)
			if err := a.setup(context.Background()); !errors.Is(err, errCancelled) {
				t.Fatalf("got %v", err)
			}
			if j.installs != 0 {
				t.Fatal("registered on EOF")
			}
			if _, e := os.Stat(a.paths.Config()); !os.IsNotExist(e) {
				t.Fatal("saved on EOF")
			}
		})
	}
}

func TestEditEnterKeepsCustomDaysAndPause(t *testing.T) {
	a, j, _ := testApp(t, "\n\n\n\n")
	old := seed(t, a, false)
	j.status = job.Status{Installed: true, Enabled: true}
	if err := a.setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := saved(t, a)
	if c.Enabled || c.ResetTime != old.ResetTime || !reflect.DeepEqual(c.ResetDays, old.ResetDays) || len(c.Providers) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestSetupSignedOutUsesNormalLoginOnly(t *testing.T) {
	a, _, _ := testApp(t, "codex\n\n\n\n\n")
	loggedIn := false
	a.auth = func(context.Context, provider.Installation) (provider.AuthStatus, error) {
		if loggedIn {
			return provider.AuthStatus{Kind: "subscription"}, nil
		}
		return provider.AuthStatus{Kind: "signed_out"}, nil
	}
	a.login = func(context.Context, provider.Installation) error { loggedIn = true; return nil }
	if err := a.setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !loggedIn {
		t.Fatal("did not offer login")
	}
}

func TestSetupRejectsAPIAndCorruptConfig(t *testing.T) {
	a, j, _ := testApp(t, "codex\n")
	a.auth = func(context.Context, provider.Installation) (provider.AuthStatus, error) {
		return provider.AuthStatus{Kind: "api", Detail: "API billing is unsupported"}, nil
	}
	if err := a.setup(context.Background()); err == nil {
		t.Fatal("accepted API login")
	}
	if j.installs != 0 {
		t.Fatal("installed despite rejected auth")
	}
	if err := os.WriteFile(a.paths.Config(), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	a.detect = func(context.Context, string) (provider.Installation, error) {
		t.Fatal("corrupt config entered setup")
		return provider.Installation{}, nil
	}
	if err := a.setup(context.Background()); err == nil {
		t.Fatal("accepted corrupt settings")
	}
}

func TestSetupRollbackRestoresWorkingSchedule(t *testing.T) {
	a, j, _ := testApp(t, "")
	old := seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	j.failInstall = true
	proposed := old
	proposed.ResetTime = "14:00"
	if err := a.apply(context.Background(), proposed, false); err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("%v", err)
	}
	if !reflect.DeepEqual(saved(t, a), old) || !j.status.Enabled {
		t.Fatal("old schedule not restored")
	}
}

func TestFirstRunFailureLeavesNoEnabledJob(t *testing.T) {
	a, j, _ := testApp(t, "\n\n\n\n")
	j.failInstall = true
	if err := a.setup(context.Background()); err == nil {
		t.Fatal("expected registration failure")
	}
	if j.status.Installed {
		t.Fatal("failed setup left job")
	}
	if _, err := os.Stat(a.paths.Config()); !os.IsNotExist(err) {
		t.Fatal("failed first run left configuration")
	}
}

func TestUpdatePreservesActivationAndDisabledJob(t *testing.T) {
	a, j, _ := testApp(t, "")
	old := seed(t, a, false)
	j.status = job.Status{Installed: true, Enabled: false}
	if err := a.update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved(t, a), old) || j.status.Enabled {
		t.Fatal("update changed saved schedule or activation")
	}
}

func TestPauseResumeDoesNotCatchUp(t *testing.T) {
	a, j, _ := testApp(t, "")
	old := seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	if err := a.enable(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if saved(t, a).Enabled {
		t.Fatal("pause not saved")
	}
	if err := a.enable(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	c := saved(t, a)
	if !c.Enabled || !c.EffectiveFrom.Equal(a.now()) || c.EffectiveFrom.Equal(old.EffectiveFrom) {
		t.Fatal("resume did not reset activation")
	}
}

func TestUninstallKeepsFilesIfJobRemovalFails(t *testing.T) {
	a, j, _ := testApp(t, "yes\n")
	seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	j.failRemove = true
	a.removeFiles = func(config.Paths) error { t.Fatal("deleted files while scheduled job exists"); return nil }
	if err := a.uninstall(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if saved(t, a).Enabled {
		t.Fatal("admission remains enabled")
	}
}

func TestPromptCancellation(t *testing.T) {
	a, _, _ := testApp(t, "")
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	a.in = bufio.NewReader(r)
	ctx, cancel := context.WithCancel(context.Background())
	a.ctx = ctx
	done := make(chan error, 1)
	go func() { _, err := a.ask("Time?", "10:00 AM"); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, errCancelled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt ignored cancellation")
	}
}

func TestLegacySetupPreservesSavedCodexPath(t *testing.T) {
	a, _, _ := testApp(t, "\n\n\n")
	path := filepath.Join(a.paths.Root, "old-install", "codex")
	legacy := config.LegacyConfig{ResetTime: "02:00", Days: []string{"Mon", "Fri"}, Enabled: false, CatchUp: true, CodexPath: path}
	if err := config.WriteJSON(a.paths.Config(), legacy); err != nil {
		t.Fatal(err)
	}
	verified := false
	a.verify = func(_ context.Context, i provider.Installation) (provider.Installation, error) {
		verified = i.Executable == path
		return i, nil
	}
	a.detect = func(_ context.Context, id string) (provider.Installation, error) {
		if id == "codex" {
			t.Error("discarded legacy path")
		}
		return provider.Installation{}, errors.New("not installed")
	}
	if err := a.setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := saved(t, a)
	if !verified || c.Providers[0].Executable != path || c.ResetTime != "02:00" || c.Enabled {
		t.Fatalf("%+v", c)
	}
}
