package runner

import (
	"github.com/OliverGrabner/agentwarmup/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func stateRecord(trigger time.Time) Record {
	reset := trigger.Add(5 * time.Hour)
	return Record{Key: "codex:" + reset.UTC().Format(time.RFC3339Nano), TriggerAt: trigger, ResetAt: reset, AttemptedAt: trigger, FinishedAt: trigger.Add(time.Second), Outcome: "succeeded"}
}
func TestStateRetentionBoundary(t *testing.T) {
	now := time.Date(2026, 9, 8, 5, 0, 0, 0, time.UTC)
	old := stateRecord(now.Add(-8*24*time.Hour - time.Second))
	boundary := stateRecord(now.Add(-8 * 24 * time.Hour))
	recent := stateRecord(now.Add(-time.Hour))
	fresh := stateRecord(now)
	s := State{SchemaVersion: 1, Records: []Record{old, boundary, recent}}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.save(path, fresh, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Records) != 3 || loaded.find(old.Key) != nil || loaded.find(boundary.Key) == nil {
		t.Fatalf("retention %+v", loaded)
	}
	if err = loaded.save(path, fresh, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if loaded.find(boundary.Key) == nil {
		t.Fatal("backward clock lost retained record")
	}
}
func TestLatestOldRecordIsRetained(t *testing.T) {
	now := time.Now().UTC()
	old := stateRecord(now.Add(-10 * 24 * time.Hour))
	old.Outcome = "missed"
	s := State{SchemaVersion: 1, Records: []Record{}}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := s.save(path, old, now); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState(path, "codex")
	if err != nil || loaded.Last == nil || loaded.Last.Key != old.Key {
		t.Fatalf("last old record lost: %+v %v", loaded, err)
	}
}
func TestStateRejectsIdentityCorruption(t *testing.T) {
	r := stateRecord(time.Date(2026, 9, 8, 5, 0, 0, 0, time.UTC))
	tests := map[string]State{
		"wrong instant key": {SchemaVersion: 1, Records: []Record{{Key: "codex:2026-09-09T10:00:00Z", ResetAt: r.ResetAt, TriggerAt: r.TriggerAt, AttemptedAt: r.AttemptedAt, FinishedAt: r.FinishedAt, Outcome: "succeeded"}}},
		"invalid last":      {SchemaVersion: 1, Records: []Record{r}, Last: &Record{Key: r.Key, Outcome: "invented"}},
		"duplicate":         {SchemaVersion: 1, Records: []Record{r, r}},
		"wrong lead":        {SchemaVersion: 1, Records: []Record{{Key: r.Key, ResetAt: r.ResetAt, TriggerAt: r.TriggerAt.Add(time.Hour), Outcome: "missed", FinishedAt: r.FinishedAt}}},
	}
	for name, s := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := config.WriteJSON(path, s); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadState(path, "codex"); err == nil {
				t.Fatal("corrupt state accepted")
			}
		})
	}
}
func TestStateMissingVsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(path, "codex")
	if err != nil || s.SchemaVersion != 1 || s.Records == nil {
		t.Fatal("missing state not initialized")
	}
	for _, data := range []string{"null", "{}", `{"schemaVersion":1,"records":null}`, `{"schemaVersion":2,"records":[]}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadState(path, "codex"); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
