package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/provider"
	"github.com/OliverGrabner/agentwarmup/internal/schedule"
)

func detectZone() (string, error) { return schedule.DetectTimezone() }

func (a *app) setup(ctx context.Context) error {
	a.ctx = ctx
	if err := a.recover(ctx); err != nil {
		return err
	}
	doc, err := config.Load(a.paths.Config())
	if err != nil && !os.IsNotExist(err) {
		return repair(err)
	}
	if err := a.jobs.Check(); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "\nAgentWarmup")
	if Preview != "false" {
		fmt.Fprintln(a.out, "\nDevelopment preview: provider reset timing is not yet verified.")
	}
	previous := doc.Current
	if doc.Legacy != nil {
		previous = &config.Config{Providers: []config.ProviderConfig{{ID: "codex", Executable: doc.Legacy.CodexPath}}}
	}
	selected, err := a.providers(ctx, previous)
	if err != nil {
		return err
	}
	reset, days, zone, enabled := "10:00", []string{"Mon", "Tue", "Wed", "Thu", "Fri"}, "", true
	if doc.Current != nil {
		reset, days, zone, enabled = doc.Current.ResetTime, doc.Current.ResetDays, doc.Current.Timezone, doc.Current.Enabled
	} else if doc.Legacy != nil {
		reset, days, enabled = doc.Legacy.ResetTime, doc.Legacy.Days, doc.Legacy.Enabled
		fmt.Fprintln(a.out, "Updating older settings. Missed warmups will now be skipped; catch-up is removed.")
	}
	reset, err = a.resetTime(reset)
	if err != nil {
		return err
	}
	days, err = a.resetDays(days)
	if err != nil {
		return err
	}
	detected, zoneErr := a.zone()
	if zone == "" && zoneErr == nil {
		zone = detected
	}
	if zone == "" || (zoneErr == nil && detected != zone) {
		if zone != "" {
			fmt.Fprintf(a.out, "Saved timezone: %s. This computer is now in %s.\n", zone, detected)
		}
		for {
			answer, err := a.ask("Timezone (for example America/Chicago):", zone)
			if err != nil {
				return err
			}
			if _, err = schedule.New(reset, days, answer); err == nil {
				zone = answer
				break
			}
			fmt.Fprintln(a.out, "Enter a valid IANA timezone, such as America/Chicago.")
		}
	}
	fmt.Fprintf(a.out, "Timezone: %s\n", zone)
	cfg := config.Config{SchemaVersion: 1, ResetTime: reset, ResetDays: days, Timezone: zone,
		Enabled: enabled, Providers: selected, EffectiveFrom: a.now().UTC()}
	if err := a.preview(cfg); err != nil {
		return err
	}
	question := "Enable schedule?"
	if !enabled {
		question = "Save schedule (keep paused)?"
	}
	yes, err := a.confirm(question, true)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintln(a.out, "Cancelled. No schedule changes saved.")
		return nil
	}
	if err := a.apply(ctx, cfg, false); err != nil {
		return err
	}
	if enabled {
		fmt.Fprintln(a.out, "\nSchedule enabled. No test request was sent.")
	} else {
		fmt.Fprintln(a.out, "\nSchedule saved and paused. No test request was sent.")
	}
	fmt.Fprintln(a.out, "Change it with agentwarmup setup.")
	return nil
}

func (a *app) providers(ctx context.Context, old *config.Config) ([]config.ProviderConfig, error) {
	type detected struct {
		inst provider.Installation
		err  error
	}
	ids := provider.IDs()
	results := make([]detected, len(ids))
	done := make(chan int, len(ids))
	for index, id := range ids {
		go func(index int, id string) {
			var existing *config.ProviderConfig
			if old != nil {
				for i := range old.Providers {
					if old.Providers[i].ID == id {
						existing = &old.Providers[i]
						break
					}
				}
			}
			if existing != nil {
				p := existing
				results[index].inst, results[index].err = a.verify(ctx, provider.Installation{ID: p.ID, Executable: p.Executable, PrefixArgs: p.PrefixArgs, Version: p.Version, ConfigRoot: p.ConfigRoot, AuthStore: p.AuthStore})
			}
			if existing == nil || results[index].err != nil {
				results[index].inst, results[index].err = a.detect(ctx, id)
			}
			done <- index
		}(index, id)
	}
	for range ids {
		<-done
	}
	var available []provider.Installation
	for i, result := range results {
		if result.err == nil {
			available = append(available, result.inst)
			fmt.Fprintf(a.out, "Found %s\n", label(ids[i]))
		} else {
			fmt.Fprintf(a.out, "%s: %s\n", label(ids[i]), provider.Scrub(result.err.Error()))
		}
	}
	if len(available) == 0 {
		fmt.Fprintln(a.out, "\nInstall or update a supported CLI, then run agentwarmup setup:")
		fmt.Fprintln(a.out, "Codex: npm install -g @openai/codex")
		fmt.Fprintln(a.out, "Claude: https://code.claude.com/docs/en/setup")
		return nil, fmt.Errorf("no supported provider CLI found")
	}
	chosen := available
	if len(available) > 1 {
		def := "both"
		if old != nil && len(old.Providers) == 1 {
			def = old.Providers[0].ID
		}
		answer, err := a.choose("Agents", []choice{{"both", "Both"}, {"codex", "Codex"}, {"claude", "Claude"}}, def, false)
		if err != nil {
			return nil, err
		}
		if answer != "both" {
			for _, inst := range available {
				if inst.ID == answer {
					chosen = []provider.Installation{inst}
				}
			}
		}
	}
	var selected []config.ProviderConfig
	for _, inst := range chosen {
		auth, err := a.auth(ctx, inst)
		if err != nil {
			return nil, fmt.Errorf("%s sign-in check failed: %s", label(inst.ID), provider.Scrub(err.Error()))
		}
		if auth.Kind == "signed_out" {
			yes, err := a.confirm("Sign in to "+label(inst.ID)+"?", true)
			if err != nil {
				return nil, err
			}
			if !yes {
				return nil, errCancelled
			}
			if err := a.login(ctx, inst); err != nil {
				return nil, err
			}
			auth, err = a.auth(ctx, inst)
			if err != nil {
				return nil, err
			}
		}
		if auth.Kind != "subscription" {
			return nil, fmt.Errorf("%s needs a supported subscription login; %s", label(inst.ID), provider.Scrub(auth.Detail))
		}
		fmt.Fprintf(a.out, "%s: signed in with a subscription\n", label(inst.ID))
		selected = append(selected, config.ProviderConfig{ID: inst.ID, Executable: inst.Executable, PrefixArgs: inst.PrefixArgs,
			ConfigRoot: inst.ConfigRoot, AuthStore: inst.AuthStore, Version: inst.Version})
	}
	return selected, nil
}

func (a *app) preview(cfg config.Config) error {
	spec, err := schedule.New(cfg.ResetTime, cfg.ResetDays, cfg.Timezone)
	if err != nil {
		return err
	}
	occ, err := spec.Next(a.now(), cfg.Providers[0].ID, 5*time.Hour)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation(cfg.Timezone)
	var ids []string
	for _, p := range cfg.Providers {
		ids = append(ids, p.ID)
	}
	fmt.Fprintf(a.out, "\nAgents: %s\n", providerLabels(ids))
	fmt.Fprintf(a.out, "\nReset days: %s\nNext warmup: %s\nTarget reset: %s\n\n", schedule.FormatDays(cfg.ResetDays), displayTime(occ.TriggerAt, loc), displayTime(occ.ResetAt, loc))
	fmt.Fprintf(a.out, "Your computer must be awake, online, and signed in at %s.\n", occ.TriggerAt.In(loc).Format("3:04 PM MST"))
	fmt.Fprint(a.out, "An active window cannot be moved. Warmups use some allowance.\n\n")
	return nil
}

func label(id string) string {
	if id == "claude" {
		return "Claude"
	}
	return "Codex"
}
func clock12(s string) string {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return s
	}
	return t.Format("3:04 PM")
}
func dayDefault(days []string) string {
	if strings.Join(days, ",") == "Mon,Tue,Wed,Thu,Fri" {
		return "weekdays"
	}
	if len(days) == 7 {
		return "everyday"
	}
	return strings.Join(days, ",")
}
func displayTime(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("Mon, Jan 2 at 3:04 PM MST")
}
