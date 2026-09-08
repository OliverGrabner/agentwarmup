package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/provider"
	"github.com/OliverGrabner/agentwarmup/internal/runner"
	"github.com/OliverGrabner/agentwarmup/internal/schedule"
)

func (a *app) status() error {
	cfg, err := a.load()
	if err != nil {
		return err
	}
	status, err := a.jobs.Inspect()
	state := "Enabled"
	for _, name := range []string{"lifecycle.json", ".uninstalling"} {
		if _, err := os.Stat(filepath.Join(a.paths.Root, name)); !os.IsNotExist(err) {
			fmt.Fprintln(a.out, "AgentWarmup needs repair. A lifecycle operation is unfinished; run agentwarmup setup.")
			return nil
		}
	}
	if !cfg.Enabled {
		state = "Paused"
	}
	if err != nil || !status.Installed || !status.Enabled {
		state = "Needs repair — run agentwarmup setup"
	}
	fmt.Fprintf(a.out, "\nAgentWarmup · %s\n", state)
	if err != nil {
		fmt.Fprintf(a.out, "Could not inspect the scheduler: %s\n", err)
	}
	loc, _ := time.LoadLocation(cfg.Timezone)
	fmt.Fprintf(a.out, "Reset: %s, %s · %s\n", clock12(cfg.ResetTime), schedule.FormatDays(cfg.ResetDays), cfg.Timezone)
	if local, err := a.zone(); err == nil && local != cfg.Timezone {
		fmt.Fprintf(a.out, "Computer timezone: %s. Your schedule keeps %s.\n", local, cfg.Timezone)
	}
	spec, _ := schedule.New(cfg.ResetTime, cfg.ResetDays, cfg.Timezone)
	at := a.now()
	if cfg.EffectiveFrom.After(at) {
		at = cfg.EffectiveFrom
	}
	occ, nextErr := spec.Next(at, cfg.Providers[0].ID, 5*time.Hour)
	if nextErr != nil {
		return nextErr
	}
	prefix := "Next warmup"
	if !cfg.Enabled || state != "Enabled" {
		prefix = "If enabled"
	}
	fmt.Fprintf(a.out, "\n%s: %s\nTarget reset: %s\n\nLast attempts\n", prefix, displayTime(occ.TriggerAt, loc), displayTime(occ.ResetAt, loc))
	included := map[string]bool{}
	for _, id := range provider.IDs() {
		included[id] = true
	}
	for _, pc := range cfg.Providers {
		if !included[pc.ID] {
			fmt.Fprintf(a.out, "  %s: unavailable in this build; run agentwarmup setup.\n", label(pc.ID))
			continue
		}
		last, err := runner.LoadState(a.paths.State(pc.ID), pc.ID)
		if err != nil {
			fmt.Fprintf(a.out, "  %s: state cannot be read; requests are blocked until repaired.\n", label(pc.ID))
		} else if last.Last == nil {
			fmt.Fprintf(a.out, "  %s: none\n", label(pc.ID))
		} else {
			record := last.Last
			when := record.AttemptedAt
			if when.IsZero() {
				when = record.FinishedAt
			}
			fmt.Fprintf(a.out, "  %s: %s — %s\n", label(pc.ID), displayTime(when, loc), describe(record.Outcome))
			if record.Detail != "" && record.Outcome != "succeeded" {
				fmt.Fprintf(a.out, "  %s\n", provider.Scrub(record.Detail))
			}
		}
	}
	fmt.Fprintln(a.out, "\nChange the schedule: agentwarmup setup")
	return nil
}

func describe(outcome string) string {
	switch outcome {
	case "succeeded":
		return "request succeeded; reset not verified"
	case "attempted", "interrupted":
		return "result unknown; will not retry"
	case "auth_required":
		return "sign-in needed; run agentwarmup setup"
	case "rate_limited":
		return "provider rate limit; will not retry"
	case "model_unavailable":
		return "model unavailable; run agentwarmup setup"
	case "timeout":
		return "request timed out; will not retry"
	case "missed":
		return "missed trigger; skipped"
	default:
		return "request failed; will not retry"
	}
}
