package cli

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/job"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLifecycleRecoversInterruptedEdit(t *testing.T) {
	a, j, _ := testApp(t, "")
	old := seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	proposed := old
	proposed.ResetTime = "14:00"
	a.ensure = func(config.Paths) (string, error) { panic("simulated process crash after staging") }
	func() {
		defer func() {
			if recover() == nil {
				t.Error("crash hook did not run")
			}
		}()
		_ = a.apply(context.Background(), proposed, false)
	}()
	if saved(t, a).Enabled || saved(t, a).ResetTime != "14:00" {
		t.Fatal("crash did not leave staged proposed config")
	}
	if _, err := os.Stat(a.journalPath()); err != nil {
		t.Fatal("crash lost recovery journal", err)
	}
	if err := a.recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved(t, a), old) {
		t.Fatal("recovery lost original schedule or activation")
	}
	if _, err := os.Stat(a.journalPath()); !os.IsNotExist(err) {
		t.Fatal("completed recovery retained journal")
	}
}
func TestRollbackFailureRetainsAdmissionGate(t *testing.T) {
	a, j, _ := testApp(t, "")
	old := seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	j.failInstall = true
	a.rollback = func(config.Paths) error { return errors.New("binary restoration unavailable") }
	if err := a.apply(context.Background(), old, false); err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("got %v", err)
	}
	if saved(t, a).Enabled {
		t.Fatal("rollback failure re-enabled admissions")
	}
	if _, err := os.Stat(a.journalPath()); err != nil {
		t.Fatal("rollback failure lost admission gate")
	}
	a.rollback = func(config.Paths) error { return nil }
	if err := a.recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved(t, a), old) {
		t.Fatal("retry did not restore original config")
	}
}
func TestRecoverCommittedTransactionFinishesCleanup(t *testing.T) {
	a, j, _ := testApp(t, "")
	old := seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	proposed := old
	proposed.ResetTime = "14:00"
	proposed.EffectiveFrom = a.now()
	if err := config.Save(a.paths.Config(), proposed); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(old)
	journal := lifecycleJournal{SchemaVersion: 1, Phase: "committing", Previous: data, Job: j.status, Committed: &proposed, CommittedJob: &j.status}
	if err := config.WriteJSON(a.journalPath(), journal); err != nil {
		t.Fatal(err)
	}
	commits := 0
	a.commit = func(config.Paths) error { commits++; return nil }
	a.rollback = func(config.Paths) error { t.Fatal("rolled back committed transaction"); return nil }
	if err := a.recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if commits != 1 || !reflect.DeepEqual(saved(t, a), proposed) {
		t.Fatal("committed transaction not finalized")
	}
}
func TestRecoverPreparedTransactionDoesNotTouchBinary(t *testing.T) {
	a, j, _ := testApp(t, "")
	old := seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	data, _ := json.Marshal(old)
	journal := lifecycleJournal{SchemaVersion: 1, Phase: "prepared", Previous: data, Job: j.status}
	if err := config.WriteJSON(a.journalPath(), journal); err != nil {
		t.Fatal(err)
	}
	a.rollback = func(config.Paths) error { t.Fatal("rollback before binary installation started"); return nil }
	if err := a.recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved(t, a), old) {
		t.Fatal("prepared transaction changed config")
	}
}
func TestUninstallExcludesConcurrentApply(t *testing.T) {
	a, j, _ := testApp(t, "yes\n")
	old := seed(t, a, true)
	j.status = job.Status{Installed: true, Enabled: true}
	removing := make(chan struct{})
	release := make(chan struct{})
	uninstalled := make(chan error, 1)
	applied := make(chan error, 1)
	a.removeFiles = func(config.Paths) error { close(removing); <-release; return nil }
	go func() { uninstalled <- a.uninstall(context.Background()) }()
	select {
	case <-removing:
	case <-time.After(3 * time.Second):
		t.Fatal("uninstall never reached cleanup")
	}
	go func() { applied <- a.apply(context.Background(), old, false) }()
	select {
	case err := <-applied:
		close(release)
		<-uninstalled
		t.Fatalf("apply entered during uninstall cleanup: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-uninstalled; err != nil {
		t.Fatal(err)
	}
	if err := <-applied; err == nil || !strings.Contains(err.Error(), "uninstall cleanup is pending") {
		t.Fatalf("apply ignored deferred cleanup: %v", err)
	}
	if j.status.Installed || j.installs != 0 {
		t.Fatal("concurrent apply recreated job during teardown")
	}
}
func TestCorruptLifecycleJournalFailsClosed(t *testing.T) {
	a, j, _ := testApp(t, "")
	seed(t, a, true)
	if err := os.WriteFile(a.journalPath(), []byte(`{"schemaVersion":`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.recover(context.Background()); err == nil {
		t.Fatal("invalid journal accepted")
	}
	if j.installs != 0 || j.removes != 0 {
		t.Fatal("invalid journal changed scheduler")
	}
}

func TestRecoveryKeepsGateWhenCommittedStateChanged(t *testing.T) {
	for _, change := range []string{"config", "job"} {
		t.Run(change, func(t *testing.T) {
			a, j, _ := testApp(t, "")
			c := seed(t, a, true)
			j.status = job.Status{Installed: true, Enabled: true}
			expected := j.status
			data, _ := json.Marshal(c)
			journal := lifecycleJournal{SchemaVersion: 1, Phase: "committing", Previous: data, Job: j.status, Committed: &c, CommittedJob: &expected}
			if err := config.WriteJSON(a.journalPath(), journal); err != nil {
				t.Fatal(err)
			}
			if change == "config" {
				changed := c
				changed.ResetTime = "14:00"
				if err := config.Save(a.paths.Config(), changed); err != nil {
					t.Fatal(err)
				}
			} else {
				j.status.Enabled = false
			}
			a.commit = func(config.Paths) error { t.Fatal("committed cleanup despite inconsistent saved state"); return nil }
			if err := a.recover(context.Background()); err == nil {
				t.Fatal("inconsistent committed state accepted")
			}
			if _, err := os.Stat(a.journalPath()); err != nil {
				t.Fatal("inconsistent state lost admission gate")
			}
		})
	}
}
