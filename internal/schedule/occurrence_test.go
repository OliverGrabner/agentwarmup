package schedule

import (
	"testing"
	"time"
)

func instant(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
func TestOccurrences(t *testing.T) {
	tests := []struct {
		name, zone, wall, from, reset, trigger string
		days                                   []string
	}{
		{"spring elapsed", "America/Chicago", "06:00", "2026-03-08T00:00:00Z", "2026-03-08T11:00:00Z", "2026-03-08T06:00:00Z", []string{"Sun"}},
		{"fall elapsed", "America/Chicago", "06:00", "2026-11-01T00:00:00Z", "2026-11-01T12:00:00Z", "2026-11-01T07:00:00Z", []string{"Sun"}},
		{"earlier fold", "America/Chicago", "01:30", "2026-11-01T00:00:00Z", "2026-11-01T06:30:00Z", "2026-11-01T01:30:00Z", []string{"Sun"}},
		{"gap skips week", "America/Chicago", "02:30", "2026-03-07T00:00:00Z", "2026-03-15T07:30:00Z", "2026-03-15T02:30:00Z", []string{"Sun"}},
		{"Sunday Monday", "UTC", "02:00", "2026-09-13T20:59:00Z", "2026-09-14T02:00:00Z", "2026-09-13T21:00:00Z", []string{"Mon"}},
		{"new year", "UTC", "00:00", "2025-12-31T18:00:00Z", "2026-01-01T00:00:00Z", "2025-12-31T19:00:00Z", []string{"Thu"}},
		{"quarter offset", "Asia/Kathmandu", "10:00", "2026-09-07T23:00:00Z", "2026-09-08T04:15:00Z", "2026-09-07T23:15:00Z", []string{"Tue"}},
		{"half hour fold", "Australia/Lord_Howe", "01:45", "2026-04-04T00:00:00Z", "2026-04-04T14:45:00Z", "2026-04-04T09:45:00Z", []string{"Sun"}},
		{"half hour gap", "Australia/Lord_Howe", "02:15", "2026-10-03T00:00:00Z", "2026-10-10T15:15:00Z", "2026-10-10T10:15:00Z", []string{"Sun"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := New(tt.wall, tt.days, tt.zone)
			if err != nil {
				t.Fatal(err)
			}
			o, err := s.Next(instant(tt.from), "codex", 5*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			if !o.ResetAt.Equal(instant(tt.reset)) || !o.TriggerAt.Equal(instant(tt.trigger)) {
				t.Fatalf("got reset %s trigger %s", o.ResetAt, o.TriggerAt)
			}
			if o.ResetAt.Sub(o.TriggerAt) != 5*time.Hour {
				t.Fatal("not five elapsed hours")
			}
			last, err := s.Latest(o.TriggerAt, "codex", 5*time.Hour)
			if err != nil || last.Key != o.Key {
				t.Fatalf("latest mismatch: %+v %v", last, err)
			}
			other, err := s.Next(instant(tt.from), "claude", 5*time.Hour)
			if err != nil || other.Key == o.Key {
				t.Fatal("provider keys must differ")
			}
		})
	}
}
func TestEligibility(t *testing.T) {
	trigger := instant("2026-09-08T10:00:00Z")
	o := Occurrence{TriggerAt: trigger}
	for _, delta := range []time.Duration{-1, 0, 119 * time.Second, 120 * time.Second, 120*time.Second + 1, 121 * time.Second} {
		want := delta >= 0 && delta <= 2*time.Minute
		if got := o.Eligible(trigger.Add(delta), trigger); got != want {
			t.Errorf("delta %s got %v", delta, got)
		}
	}
	if o.Eligible(trigger, trigger.Add(1)) {
		t.Fatal("activation after trigger admitted")
	}
}
func TestSavedZoneTravel(t *testing.T) {
	s, _ := New("06:00", []string{"Sun"}, "America/Chicago")
	now := instant("2026-03-08T00:00:00Z")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	a, _ := s.Next(now, "codex", 5*time.Hour)
	b, _ := s.Next(now.In(tokyo), "codex", 5*time.Hour)
	if a.Key != b.Key || !a.TriggerAt.Equal(b.TriggerAt) {
		t.Fatal("travel changed saved schedule")
	}
}
func TestWindowsMapping(t *testing.T) {
	for _, key := range []string{"Central Standard Time", "Nepal Standard Time", "Lord Howe Standard Time"} {
		zone, ok := windowsZones[key]
		if !ok {
			t.Fatal(key)
		}
		if _, err := time.LoadLocation(zone); err != nil {
			t.Fatal(err)
		}
	}
	if windowsZones["Central Standard Time"] != "America/Chicago" {
		t.Fatal("incorrect central mapping")
	}
}
func TestInvalidSpec(t *testing.T) {
	for _, v := range []struct {
		wall, zone string
		days       []string
	}{{"6:00", "UTC", []string{"Mon"}}, {"06:00", "Local", []string{"Mon"}}, {"06:00", "UTC", nil}, {"06:00", "UTC", []string{"Mon", "Mon"}}, {"06:00", "CST", []string{"Mon"}}} {
		if _, err := New(v.wall, v.days, v.zone); err == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
}
