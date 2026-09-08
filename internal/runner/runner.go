package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/provider"
	"github.com/OliverGrabner/agentwarmup/internal/schedule"
)

type Runner struct {
	Paths   config.Paths
	Now     func() time.Time
	Prepare func(context.Context, config.ProviderConfig, string) (provider.Invocation, provider.Result)
	Execute func(context.Context, provider.Invocation) provider.Result
	Timeout time.Duration
}

func New(paths config.Paths) *Runner {
	return &Runner{Paths: paths, Now: time.Now, Prepare: prepare, Execute: Execute, Timeout: 2 * time.Minute}
}

func installation(pc config.ProviderConfig) provider.Installation {
	return provider.Installation{ID: pc.ID, Executable: pc.Executable, PrefixArgs: pc.PrefixArgs, ConfigRoot: pc.ConfigRoot, AuthStore: pc.AuthStore, Version: pc.Version}
}

func prepare(ctx context.Context, pc config.ProviderConfig, dir string) (provider.Invocation, provider.Result) {
	inst := installation(pc)
	if _, err := os.Stat(inst.Executable); os.IsNotExist(err) {
		found, findErr := provider.Detect(ctx, pc.ID)
		if findErr != nil {
			return provider.Invocation{}, provider.Result{Outcome: "failed", Detail: "CLI not found; run agentwarmup setup."}
		}
		found.ConfigRoot, found.AuthStore = pc.ConfigRoot, pc.AuthStore
		inst = found
	}
	verified, err := provider.Verify(ctx, inst)
	if err != nil {
		return provider.Invocation{}, provider.Result{Outcome: "failed", Detail: provider.Scrub(err.Error())}
	}
	inst = verified
	auth, err := provider.CheckAuth(ctx, inst)
	if err != nil {
		return provider.Invocation{}, provider.Result{Outcome: "auth_required", Detail: provider.Scrub(err.Error())}
	}
	if auth.Kind != "subscription" {
		return provider.Invocation{}, provider.Result{Outcome: "auth_required", Detail: "Subscription sign-in needed; run agentwarmup setup."}
	}
	inv, err := provider.Request(inst, dir)
	if err != nil {
		return inv, provider.Result{Outcome: "failed", Detail: provider.Scrub(err.Error())}
	}
	return inv, provider.Result{}
}

func current(paths config.Paths) (*config.Config, error) {
	for _, name := range []string{"lifecycle.json", ".uninstalling"} {
		if _, err := os.Stat(filepath.Join(paths.Root, name)); !os.IsNotExist(err) {
			return nil, fmt.Errorf("lifecycle operation unfinished; run agentwarmup setup")
		}
	}
	doc, err := config.Load(paths.Config())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if doc.Current == nil {
		return nil, errors.New("configuration needs migration; run agentwarmup setup")
	}
	return doc.Current, nil
}

func (r *Runner) Check(ctx context.Context) error {
	cfg, err := current(r.Paths)
	if err != nil || cfg == nil || !cfg.Enabled {
		return err
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(cfg.Providers))
	for _, pc := range cfg.Providers {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := r.checkProvider(ctx, id); err != nil {
				errs <- fmt.Errorf("%s: %w", id, err)
			}
		}(pc.ID)
	}
	wg.Wait()
	close(errs)
	var result error
	for err := range errs {
		result = errors.Join(result, err)
	}
	return result
}

func (r *Runner) occurrence(id string) (*config.Config, config.ProviderConfig, schedule.Occurrence, error) {
	var pc config.ProviderConfig
	var occurrence schedule.Occurrence
	cfg, err := current(r.Paths)
	if err != nil || cfg == nil || !cfg.Enabled {
		return nil, pc, occurrence, err
	}
	for _, item := range cfg.Providers {
		if item.ID == id {
			pc = item
		}
	}
	if pc.ID == "" {
		return nil, pc, occurrence, nil
	}
	spec, err := schedule.New(cfg.ResetTime, cfg.ResetDays, cfg.Timezone)
	if err != nil {
		return nil, pc, occurrence, err
	}
	occurrence, err = spec.Latest(r.Now(), id, 5*time.Hour)
	if err != nil {
		return nil, pc, occurrence, err
	}
	if occurrence.TriggerAt.Before(cfg.EffectiveFrom) {
		return nil, pc, occurrence, nil
	}
	return cfg, pc, occurrence, nil
}

func (r *Runner) checkProvider(ctx context.Context, id string) error {
	// Compute first: idle minute checks must not launch CLI processes.
	cfg, pc, occurrence, err := r.occurrence(id)
	if err != nil || cfg == nil {
		return err
	}
	lock, err := TryLock(r.Paths.Lock(id))
	if errors.Is(err, ErrLocked) {
		return nil
	}
	if err != nil {
		return err
	}
	defer lock.Close()
	state, err := LoadState(r.Paths.State(id), id)
	if err != nil {
		return err
	}
	if previous := state.find(occurrence.Key); previous != nil {
		if previous.Outcome == "attempted" {
			record := *previous
			record.Outcome, record.FinishedAt = "interrupted", r.Now()
			record.Detail = "Previous attempt did not finish; it will not be retried."
			return state.save(r.Paths.State(id), record, r.Now())
		}
		return nil
	}
	if !occurrence.Eligible(r.Now(), cfg.EffectiveFrom) {
		return r.saveMissed(ctx, id, &state, occurrence)
	}
	runDir := r.Paths.RunDir(id)
	if err := os.MkdirAll(runDir, 0700); err != nil {
		return err
	}
	remaining := occurrence.TriggerAt.Add(2 * time.Minute).Sub(r.Now())
	prepCtx, cancel := context.WithTimeout(ctx, min(remaining+time.Nanosecond, 20*time.Second))
	inv, prepResult := r.Prepare(prepCtx, pc, runDir)
	cancel()
	var admitted bool
	var record Record
	err = WithLock(ctx, r.Paths.Lock("lifecycle"), func() error {
		latest, _, due, err := r.occurrence(id)
		if err != nil || latest == nil {
			return err
		}
		if due.Key != occurrence.Key {
			return nil
		}
		now := r.Now()
		record = Record{Key: due.Key, ResetAt: due.ResetAt, TriggerAt: due.TriggerAt}
		if !due.Eligible(now, latest.EffectiveFrom) {
			record.Outcome, record.FinishedAt = "missed", now
			return state.save(r.Paths.State(id), record, now)
		}
		if prepResult.Outcome != "" {
			record.Outcome, record.Detail, record.FinishedAt = prepResult.Outcome, provider.Scrub(prepResult.Detail), now
			return state.save(r.Paths.State(id), record, now)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		record.Outcome, record.AttemptedAt = "attempted", now
		if err := state.save(r.Paths.State(id), record, now); err != nil {
			return err
		}
		admitted = true
		return nil
	})
	if err != nil || !admitted {
		return err
	}
	// A slow state write must not turn into a request outside the admission interval.
	if !occurrence.Eligible(r.Now(), cfg.EffectiveFrom) {
		record.Outcome, record.FinishedAt = "missed", r.Now()
		return state.save(r.Paths.State(id), record, r.Now())
	}
	runCtx, stop := context.WithTimeout(ctx, r.Timeout)
	result := r.Execute(runCtx, inv)
	stop()
	record.Outcome, record.Detail, record.FinishedAt = result.Outcome, provider.Scrub(result.Detail), r.Now()
	if !validOutcome(record.Outcome) || record.Outcome == "attempted" {
		record.Outcome, record.Detail = "failed", "Provider returned an unknown result."
	}
	return state.save(r.Paths.State(id), record, r.Now())
}

func (r *Runner) saveMissed(ctx context.Context, id string, state *State, occurrence schedule.Occurrence) error {
	return WithLock(ctx, r.Paths.Lock("lifecycle"), func() error {
		cfg, _, latest, err := r.occurrence(id)
		if err != nil || cfg == nil || latest.Key != occurrence.Key {
			return err
		}
		now := r.Now()
		if latest.Eligible(now, cfg.EffectiveFrom) {
			return nil
		}
		return state.save(r.Paths.State(id), Record{Key: latest.Key, ResetAt: latest.ResetAt, TriggerAt: latest.TriggerAt, FinishedAt: now, Outcome: "missed"}, now)
	})
}
