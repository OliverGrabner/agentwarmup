package schedule

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

type Spec struct {
	ResetTime string
	ResetDays []string
	Location  *time.Location
}
type Occurrence struct {
	ResetAt   time.Time
	TriggerAt time.Time
	Key       string
}

func New(resetTime string, resetDays []string, zone string) (Spec, error) {
	normalized, err := ParseTime(resetTime)
	if err != nil || normalized != resetTime {
		return Spec{}, fmt.Errorf("reset time must be normalized HH:MM")
	}
	if len(resetDays) == 0 {
		return Spec{}, fmt.Errorf("no reset days")
	}
	seen := map[string]bool{}
	for _, d := range resetDays {
		if dayIndex(d) < 0 || seen[d] {
			return Spec{}, fmt.Errorf("invalid or duplicate reset day %q", d)
		}
		seen[d] = true
	}
	if zone != "UTC" && !strings.Contains(zone, "/") {
		return Spec{}, fmt.Errorf("an explicit IANA timezone is required")
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Spec{}, err
	}
	return Spec{resetTime, append([]string(nil), resetDays...), loc}, nil
}

// Resolve offsets over nearby zone intervals, then round-trip wall components.
// This rejects gaps and deterministically chooses the first instant in a fold.
func (s Spec) reset(date time.Time) (time.Time, bool) {
	h, m, _ := splitTime(s.ResetTime)
	wall := time.Date(date.Year(), date.Month(), date.Day(), h, m, 0, 0, time.UTC)
	end := wall.Add(48 * time.Hour)
	cursor := wall.Add(-48 * time.Hour)
	var best time.Time
	for cursor.Before(end) {
		local := cursor.In(s.Location)
		_, offset := local.Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		back := candidate.In(s.Location)
		if back.Year() == wall.Year() && back.Month() == wall.Month() && back.Day() == wall.Day() && back.Hour() == h && back.Minute() == m && back.Second() == 0 && (best.IsZero() || candidate.Before(best)) {
			best = candidate
		}
		_, bound := local.ZoneBounds()
		if bound.IsZero() || !bound.After(cursor) {
			break
		}
		cursor = bound
	}
	return best.In(s.Location), !best.IsZero()
}

func (s Spec) search(at time.Time, id string, lead time.Duration, next bool) (Occurrence, error) {
	if s.Location == nil || id == "" || lead <= 0 || lead > 24*time.Hour {
		return Occurrence{}, fmt.Errorf("invalid occurrence parameters")
	}
	local := at.In(s.Location)
	date := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, time.UTC)
	var best Occurrence
	for i := -16; i <= 16; i++ {
		d := date.AddDate(0, 0, i)
		wanted := false
		for _, day := range s.ResetDays {
			if dayIndex(day) == int(d.Weekday()) {
				wanted = true
			}
		}
		if !wanted {
			continue
		}
		reset, ok := s.reset(d)
		if !ok {
			continue
		}
		trigger := reset.Add(-lead)
		if next && trigger.Before(at) || !next && trigger.After(at) {
			continue
		}
		if best.Key == "" || next && trigger.Before(best.TriggerAt) || !next && trigger.After(best.TriggerAt) {
			best = Occurrence{reset, trigger, id + ":" + reset.UTC().Format(time.RFC3339Nano)}
		}
	}
	if best.Key == "" {
		return Occurrence{}, fmt.Errorf("no valid occurrence within sixteen days")
	}
	return best, nil
}
func (s Spec) Next(at time.Time, id string, lead time.Duration) (Occurrence, error) {
	return s.search(at, id, lead, true)
}
func (s Spec) Latest(at time.Time, id string, lead time.Duration) (Occurrence, error) {
	return s.search(at, id, lead, false)
}
func (o Occurrence) Eligible(now, effectiveFrom time.Time) bool {
	return !o.TriggerAt.Before(effectiveFrom) && !now.Before(o.TriggerAt) && !now.After(o.TriggerAt.Add(2*time.Minute))
}
