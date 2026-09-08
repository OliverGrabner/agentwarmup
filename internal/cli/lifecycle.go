package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/job"
	"github.com/OliverGrabner/agentwarmup/internal/provider"
	"github.com/OliverGrabner/agentwarmup/internal/runner"
)

const journalName = "lifecycle.json"
const uninstallName = ".uninstalling"

type lifecycleJournal struct {
	SchemaVersion int             `json:"schemaVersion"`
	Phase         string          `json:"phase"`
	Previous      json.RawMessage `json:"previous"`
	Job           job.Status      `json:"job"`
	Committed     *config.Config  `json:"committed,omitempty"`
	CommittedJob  *job.Status     `json:"committedJob,omitempty"`
}

func (a *app) journalPath() string { return filepath.Join(a.paths.Root, journalName) }
func (a *app) pendingUninstall() error {
	if _, err := os.Stat(filepath.Join(a.paths.Root, uninstallName)); err == nil {
		return errors.New("uninstall cleanup is pending; rerun agentwarmup uninstall to finish")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func (a *app) operation(ctx context.Context, fn func() error) error {
	return runner.WithLock(ctx, a.paths.OperationLock(), func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn()
	})
}

// recover restores a durable interrupted transaction before setup reads defaults.
func (a *app) recover(ctx context.Context) error {
	return a.operation(ctx, func() error {
		return runner.WithLock(ctx, a.paths.Lock("lifecycle"), func() error {
			if err := a.recoverLocked(); err != nil {
				return err
			}
			return a.pendingUninstall()
		})
	})
}
func (a *app) recoverLocked() error {
	data, err := os.ReadFile(a.journalPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var j lifecycleJournal
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("invalid lifecycle journal; admissions remain disabled: %w", err)
	}
	for _, key := range []string{"schemaVersion", "phase", "previous", "job"} {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("incomplete lifecycle journal (%s); admissions remain disabled", key)
		}
	}
	var oldJobFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["job"], &oldJobFields); err != nil {
		return errors.New("invalid previous job in lifecycle journal")
	}
	for _, key := range []string{"Installed", "Enabled"} {
		if value, ok := oldJobFields[key]; !ok || string(value) == "null" {
			return errors.New("incomplete previous job in lifecycle journal")
		}
	}
	if err = json.Unmarshal(data, &j); err != nil {
		return fmt.Errorf("invalid lifecycle journal; admissions remain disabled: %w", err)
	}
	if j.SchemaVersion != 1 || (j.Phase != "prepared" && j.Phase != "installing" && j.Phase != "committing" && j.Phase != "restoring") {
		return errors.New("unsupported lifecycle journal; admissions remain disabled")
	}
	if string(j.Previous) == "null" {
		j.Previous = nil
	}
	if len(j.Previous) > 0 && !json.Valid(j.Previous) {
		return errors.New("invalid previous configuration in lifecycle journal")
	}
	if j.Phase == "committing" {
		// Config and verified job have committed; finish idempotent PATH/backup cleanup.
		if err := a.verifyCommitted(j); err != nil {
			return fmt.Errorf("committed installation needs repair; admissions remain disabled: %w", err)
		}
		if err := a.commit(a.paths); err == nil {
			return os.Remove(a.journalPath())
		}
	}
	if err := a.restoreLocked(j); err != nil {
		return fmt.Errorf("interrupted update recovery incomplete; admissions remain disabled: %w", err)
	}
	return nil
}

func (a *app) verifyCommitted(j lifecycleJournal) error {
	if j.Committed == nil || j.CommittedJob == nil {
		return errors.New("journal is missing committed configuration or job")
	}
	doc, err := config.Load(a.paths.Config())
	if err != nil {
		return err
	}
	if doc.Current == nil || !reflect.DeepEqual(doc.Current, j.Committed) {
		return errors.New("saved configuration differs from committed transaction")
	}
	status, err := a.jobs.Inspect()
	if err != nil {
		return err
	}
	if status.Installed != j.CommittedJob.Installed || status.Enabled != j.CommittedJob.Enabled {
		return errors.New("scheduled job differs from committed transaction")
	}
	return nil
}

// restoreLocked never restores an enabled configuration until binary and job
// restoration have succeeded. The retained journal is also an admission gate.
func (a *app) restoreLocked(j lifecycleJournal) error {
	// Once rollback begins, recovery must never mistake the partial restoration
	// for a transaction whose new configuration should still be committed.
	if j.Phase != "prepared" && j.Phase != "restoring" {
		j.Phase = "restoring"
		if err := config.WriteJSON(a.journalPath(), j); err != nil {
			return err
		}
	}
	var result error
	if j.Phase != "prepared" {
		result = a.rollback(a.paths)
	}
	if j.Job.Installed {
		result = errors.Join(result, a.jobs.Install(a.paths.Bin()))
		if !j.Job.Enabled {
			result = errors.Join(result, a.jobs.SetEnabled(false))
		}
	} else {
		result = errors.Join(result, a.jobs.Uninstall())
	}
	if result == nil {
		got, err := a.jobs.Inspect()
		result = err
		if err == nil && (got.Installed != j.Job.Installed || (got.Installed && got.Enabled != j.Job.Enabled)) {
			result = errors.New("restored job could not be verified")
		}
	}
	if result != nil {
		doc, err := config.Load(a.paths.Config())
		if err == nil && doc.Current != nil {
			c := *doc.Current
			c.Enabled = false
			result = errors.Join(result, config.Save(a.paths.Config(), c))
		}
		return result
	}
	if len(j.Previous) == 0 {
		err := os.Remove(a.paths.Config())
		if !os.IsNotExist(err) {
			result = err
		}
	} else {
		result = config.WriteJSON(a.paths.Config(), j.Previous)
	}
	if result != nil {
		return result
	}
	return os.Remove(a.journalPath())
}

func (a *app) apply(ctx context.Context, proposed config.Config, update bool) error {
	return a.operation(ctx, func() error {
		return runner.WithLock(ctx, a.paths.Lock("lifecycle"), func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := a.recoverLocked(); err != nil {
				return err
			}
			if err := a.pendingUninstall(); err != nil {
				return err
			}
			previous, err := os.ReadFile(a.paths.Config())
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			oldJob, err := a.jobs.Inspect()
			if err != nil {
				return fmt.Errorf("inspect existing schedule: %w", err)
			}
			if update {
				current, err := a.load()
				if err != nil {
					return err
				}
				proposed = *current
			}
			if err := proposed.Validate(); err != nil {
				return err
			}
			j := lifecycleJournal{SchemaVersion: 1, Phase: "prepared", Previous: previous, Job: oldJob}
			if err := config.WriteJSON(a.journalPath(), j); err != nil {
				return err
			}
			rollback := func(cause error) error {
				if err := a.restoreLocked(j); err != nil {
					return fmt.Errorf("%w; rollback incomplete: %v; admissions remain disabled; run agentwarmup setup to repair", cause, err)
				}
				return fmt.Errorf("%w; previous schedule restored", cause)
			}
			staged := proposed
			staged.Enabled = false
			if err := config.Save(a.paths.Config(), staged); err != nil {
				return rollback(err)
			}
			j.Phase = "installing"
			if err := config.WriteJSON(a.journalPath(), j); err != nil {
				j.Phase = "prepared"
				return rollback(err)
			}
			exe, err := a.ensure(a.paths)
			if err != nil {
				return rollback(err)
			}
			if err = a.jobs.Install(exe); err != nil {
				return rollback(fmt.Errorf("register schedule: %w", err))
			}
			if update && oldJob.Installed && !oldJob.Enabled {
				if err := a.jobs.SetEnabled(false); err != nil {
					return rollback(err)
				}
			}
			verified, err := a.jobs.Inspect()
			expectEnabled := !update || !oldJob.Installed || oldJob.Enabled
			if err != nil {
				return rollback(err)
			}
			if !verified.Installed || verified.Enabled != expectEnabled {
				return rollback(errors.New("scheduler registration could not be verified"))
			}
			if !update {
				proposed.EffectiveFrom = a.now().UTC()
			}
			if err := config.Save(a.paths.Config(), proposed); err != nil {
				return rollback(err)
			}
			j.Phase = "committing"
			j.Committed = &proposed
			j.CommittedJob = &verified
			if err := config.WriteJSON(a.journalPath(), j); err != nil {
				return rollback(err)
			}
			if err := a.commit(a.paths); err != nil {
				return rollback(fmt.Errorf("finish installation: %w", err))
			}
			if err := os.Remove(a.journalPath()); err != nil {
				return fmt.Errorf("installation committed; cleanup needs recovery: %w", err)
			}
			return nil
		})
	})
}

func (a *app) update(ctx context.Context) error {
	if err := a.recover(ctx); err != nil {
		return err
	}
	doc, err := config.Load(a.paths.Config())
	if os.IsNotExist(err) || (err == nil && doc.Legacy != nil) {
		return a.setup(ctx)
	}
	if err != nil {
		return repair(err)
	}
	if err := a.jobs.Check(); err != nil {
		return err
	}
	if err := a.apply(ctx, *doc.Current, true); err != nil {
		return err
	}
	fmt.Fprintln(a.out, "AgentWarmup updated. Your schedule is unchanged.")
	return a.status()
}

func (a *app) enable(ctx context.Context, enabled bool) error {
	err := a.operation(ctx, func() error {
		return runner.WithLock(ctx, a.paths.Lock("lifecycle"), func() error {
			if err := a.recoverLocked(); err != nil {
				return err
			}
			if err := a.pendingUninstall(); err != nil {
				return err
			}
			cfg, err := a.load()
			if err != nil {
				return err
			}
			if enabled {
				status, err := a.jobs.Inspect()
				if err != nil {
					return err
				}
				if !status.Installed || !status.Enabled {
					return errors.New("scheduled job needs repair; run agentwarmup setup")
				}
				if cfg.Enabled {
					return nil
				}
				cfg.EffectiveFrom = a.now().UTC()
			}
			cfg.Enabled = enabled
			return config.Save(a.paths.Config(), *cfg)
		})
	})
	if err != nil {
		return err
	}
	if enabled {
		fmt.Fprintln(a.out, "Warmups resumed.")
		return a.status()
	}
	fmt.Fprintln(a.out, "Warmups paused. A request already underway may finish.\nResume: agentwarmup resume")
	return nil
}

func (a *app) uninstall(ctx context.Context) error {
	yes, err := a.confirm("Remove AgentWarmup, its schedule, and settings?", false)
	if err != nil {
		return err
	}
	if !yes {
		fmt.Fprintln(a.out, "Cancelled.")
		return nil
	}
	err = a.operation(ctx, func() error {
		err := runner.WithLock(ctx, a.paths.Lock("lifecycle"), func() error {
			// A teardown can finish a prior failed teardown. It must not erase an
			// unresolved transaction whose binary/configuration backup is still needed.
			if err := a.recoverLocked(); err != nil {
				return err
			}
			token := make([]byte, 16)
			if _, err := rand.Read(token); err != nil {
				return err
			}
			if err := config.WriteJSON(filepath.Join(a.paths.Root, uninstallName), hex.EncodeToString(token)); err != nil {
				return err
			}
			doc, err := config.Load(a.paths.Config())
			if err == nil && doc.Current != nil {
				c := *doc.Current
				c.Enabled = false
				if err := config.Save(a.paths.Config(), c); err != nil {
					return err
				}
			}
			if err := a.jobs.Uninstall(); err != nil {
				return fmt.Errorf("schedule removal failed; files retained: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		// Retain operation exclusion while releasing lifecycle admission: checks
		// waiting for admission can exit and release their provider locks.
		waitCtx, cancel := context.WithTimeout(ctx, 150*time.Second)
		defer cancel()
		for _, id := range provider.AllIDs() {
			if err := runner.WithLock(waitCtx, a.paths.Lock(id), func() error { return nil }); err != nil {
				return fmt.Errorf("schedule removed; waiting for active requests failed, files retained: %w", err)
			}
		}
		if err := a.removeFiles(a.paths); err != nil {
			return fmt.Errorf("schedule removed; file cleanup failed: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(a.out, "Schedule removed. File cleanup completes when AgentWarmup exits.")
	} else {
		fmt.Fprintln(a.out, "AgentWarmup removed.")
	}
	return nil
}
