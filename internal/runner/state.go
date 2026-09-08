package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/config"
)

type Record struct {
	Key         string    `json:"key"`
	ResetAt     time.Time `json:"resetAt"`
	TriggerAt   time.Time `json:"triggerAt"`
	AttemptedAt time.Time `json:"attemptedAt,omitempty"`
	FinishedAt  time.Time `json:"finishedAt,omitempty"`
	Outcome     string    `json:"outcome"`
	Detail      string    `json:"detail,omitempty"`
}

type State struct {
	SchemaVersion int      `json:"schemaVersion"`
	Records       []Record `json:"records"`
	Last          *Record  `json:"last,omitempty"`
}

func LoadState(path, id string) (State, error) {
	s := State{SchemaVersion: 1, Records: []Record{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s = State{}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("invalid %s attempt state: %w", id, err)
	}
	if s.SchemaVersion != 1 || s.Records == nil {
		return s, fmt.Errorf("unsupported or incomplete %s attempt state", id)
	}
	seen := make(map[string]bool)
	for _, record := range s.Records {
		if record.Key != id+":"+record.ResetAt.UTC().Format(time.RFC3339Nano) || record.ResetAt.IsZero() || record.TriggerAt.IsZero() || record.ResetAt.Sub(record.TriggerAt) != 5*time.Hour || !validOutcome(record.Outcome) || seen[record.Key] {
			return s, fmt.Errorf("invalid %s occurrence record", id)
		}
		seen[record.Key] = true
	}
	if len(s.Records) > 0 && s.Last == nil {
		return s, fmt.Errorf("missing %s last occurrence", id)
	}
	if s.Last != nil && (!seen[s.Last.Key] || !sameRecord(*s.find(s.Last.Key), *s.Last)) {
		return s, fmt.Errorf("invalid %s last occurrence", id)
	}
	return s, nil
}

func sameRecord(a, b Record) bool {
	return a.Key == b.Key && a.ResetAt.Equal(b.ResetAt) && a.TriggerAt.Equal(b.TriggerAt) && a.AttemptedAt.Equal(b.AttemptedAt) && a.FinishedAt.Equal(b.FinishedAt) && a.Outcome == b.Outcome && a.Detail == b.Detail
}

func validOutcome(outcome string) bool {
	switch outcome {
	case "attempted", "succeeded", "auth_required", "rate_limited", "model_unavailable", "timeout", "failed", "missed", "interrupted":
		return true
	}
	return false
}

func (s *State) find(key string) *Record {
	for i := range s.Records {
		if s.Records[i].Key == key {
			return &s.Records[i]
		}
	}
	return nil
}

func (s *State) save(path string, record Record, now time.Time) error {
	if existing := s.find(record.Key); existing != nil {
		*existing = record
	} else {
		s.Records = append(s.Records, record)
	}
	s.Last = &record
	cutoff := now.Add(-8 * 24 * time.Hour)
	kept := s.Records[:0]
	for _, item := range s.Records {
		if !item.TriggerAt.Before(cutoff) || item.Key == record.Key {
			kept = append(kept, item)
		}
	}
	s.Records = kept
	return config.WriteJSON(path, s)
}
