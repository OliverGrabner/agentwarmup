package cli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/job"
	"golang.org/x/term"
)

func rawTestApp(t *testing.T, keys string) (*app, *int, *int) {
	t.Helper()
	a, _, _ := testApp(t, keys)
	starts, restores := 0, 0
	a.startRaw = func() (func() error, error) {
		starts++
		return func() error { restores++; return nil }, nil
	}
	return a, &starts, &restores
}

func TestMenuNavigationAndRestoration(t *testing.T) {
	for _, tt := range []struct{ name, keys, def, want string }{
		{"default", "\r", "both", "both"},
		{"saved", "\r", "claude", "claude"},
		{"down", "\x1b[B\r", "both", "codex"},
		{"up wraps", "\x1bOA\r", "both", "claude"},
		{"number", "2\r", "both", "codex"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a, starts, restores := rawTestApp(t, tt.keys)
			got, err := a.choose("Agents", []choice{{"both", "Both"}, {"codex", "Codex"}, {"claude", "Claude"}}, tt.def, false)
			if err != nil || got != tt.want || *starts != 1 || *restores != 1 {
				t.Fatalf("got %q, %v; mode %d/%d", got, err, *starts, *restores)
			}
		})
	}
}

func TestTerminalCancellationRestoresMode(t *testing.T) {
	for _, keys := range []string{"", "\x03", "\x04", "\x1a", "\x1b"} {
		t.Run(strings.ReplaceAll(keys, "\x1b", "escape"), func(t *testing.T) {
			a, starts, restores := rawTestApp(t, keys)
			if _, err := a.resetTime("10:00"); !errors.Is(err, errCancelled) {
				t.Fatalf("%v", err)
			}
			if *starts != *restores || *starts != 1 {
				t.Fatalf("mode %d/%d", *starts, *restores)
			}
		})
	}
	// Escape and signals must also work while terminal input remains open.
	for _, escape := range []bool{true, false} {
		a, starts, restores := rawTestApp(t, "")
		r, w := io.Pipe()
		a.in = bufio.NewReader(r)
		ctx, cancel := context.WithCancel(context.Background())
		a.ctx = ctx
		done := make(chan error, 1)
		go func() { _, err := a.resetTime("10:00"); done <- err }()
		if escape {
			_, _ = w.Write([]byte{27})
		} else {
			cancel()
		}
		select {
		case err := <-done:
			if !errors.Is(err, errCancelled) || *starts != *restores {
				t.Fatalf("%v; mode %d/%d", err, *starts, *restores)
			}
		case <-time.After(time.Second):
			t.Fatal("cancellation blocked")
		}
		cancel()
		_ = r.Close()
		_ = w.Close()
	}
}

func TestCustomTimeAndDaySelection(t *testing.T) {
	a, starts, restores := rawTestApp(t, "\x1b[A\x1b[A\r9:35 AM\x7f\x7fAM\r")
	got, err := a.resetTime("10:00")
	if err != nil || got != "09:35" || *starts != 2 || *restores != 2 {
		t.Fatalf("%q %v %d/%d", got, err, *starts, *restores)
	}
	a, _, _ = rawTestApp(t, "\r\r")
	got, err = a.resetTime("02:35")
	if err != nil || got != "02:35" {
		t.Fatalf("%q %v", got, err)
	}
	days, err := a.resetDays([]string{"Mon", "Wed", "Fri"})
	if err != nil || !reflect.DeepEqual(days, []string{"Mon", "Wed", "Fri"}) {
		t.Fatalf("%v %v", days, err)
	}
	// Open custom picker, remove Monday, add Saturday.
	a, _, _ = rawTestApp(t, "\x1b[A\r \x1b[A\x1b[A \r")
	days, err = a.resetDays([]string{"Mon", "Tue", "Wed", "Thu", "Fri"})
	if err != nil || !reflect.DeepEqual(days, []string{"Tue", "Wed", "Thu", "Fri", "Sat"}) {
		t.Fatalf("%v %v", days, err)
	}
	// Empty selections stay in the picker until at least one day is selected.
	a, _, _ = rawTestApp(t, " \r\x1b[B \r")
	days, err = a.pickDays([]string{"Mon"})
	if err != nil || !reflect.DeepEqual(days, []string{"Tue"}) {
		t.Fatalf("%v %v", days, err)
	}
	a, _, _ = rawTestApp(t, "\x1b[A \r")
	days, err = a.pickDays([]string{"Mon"})
	if err != nil || !reflect.DeepEqual(days, []string{"Sun", "Mon"}) {
		t.Fatalf("Sunday must use config's canonical order: %v %v", days, err)
	}
}

func TestTerminalSetupCancellationLeavesScheduleUnchanged(t *testing.T) {
	for _, keys := range []string{"\x1b", "\r\x03", "\r\r\x04", "\r\r\r\x1b"} {
		a, j, _ := testApp(t, keys)
		old := seed(t, a, true)
		j.status = job.Status{Installed: true, Enabled: true}
		starts, restores := 0, 0
		a.startRaw = func() (func() error, error) { starts++; return func() error { restores++; return nil }, nil }
		if err := a.setup(context.Background()); !errors.Is(err, errCancelled) {
			t.Fatalf("%q: %v", keys, err)
		}
		if !reflect.DeepEqual(saved(t, a), old) || j.installs != 0 || starts != restores {
			t.Fatalf("changed state on %q", keys)
		}
	}
}

func TestTerminalSetupDefaultsNoAdmission(t *testing.T) {
	a, j, out := testApp(t, "\r\r\r\r")
	a.startRaw = func() (func() error, error) { return func() error { return nil }, nil }
	j.duringInstall = func() {
		if saved(t, a).Enabled {
			t.Fatal("admitted during setup")
		}
	}
	if err := a.setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := saved(t, a)
	if c.ResetTime != "10:00" || len(c.ResetDays) != 5 || len(c.Providers) != 2 || !strings.Contains(out.String(), "Agents: Codex, Claude") {
		t.Fatalf("%+v\n%s", c, out)
	}
}

func TestPlainMenusAndUnsupportedRawFallback(t *testing.T) {
	a, _, out := testApp(t, "2\n9:30 AM\nweekends\ny\n")
	a.startRaw = func() (func() error, error) { return nil, errors.New("unsupported terminal") }
	if err := a.setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := saved(t, a)
	if len(c.Providers) != 1 || c.Providers[0].ID != "codex" || c.ResetTime != "09:30" || !reflect.DeepEqual(c.ResetDays, []string{"Sun", "Sat"}) {
		t.Fatalf("%+v", c)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatal("escape codes in plain output")
	}
	for _, key := range []string{"\x03\n", "\x1b\n", "\x04\n"} {
		a, _, _ := testApp(t, key)
		if _, err := a.ask("Time", "10:00"); !errors.Is(err, errCancelled) {
			t.Fatal(err)
		}
	}
}

// This opt-in preview uses only fake providers/jobs and an isolated test root.
// Compile with go test -c, set AGENTWARMUP_TEST_TERMINAL=1, then run the test
// executable directly with -test.run=^TestTerminalPreview$ -test.v. The go test
// driver captures stdout, which correctly selects the plain fallback.
func TestTerminalPreview(t *testing.T) {
	if os.Getenv("AGENTWARMUP_TEST_TERMINAL") != "1" {
		t.Skip("interactive fake-only preview")
	}
	a, _, _ := testApp(t, "")
	a.in, a.out = nil, os.Stdout
	defer a.closeTerminal()
	if err := a.input(); err != nil {
		t.Fatal(err)
	}
	if a.startRaw == nil {
		width, height, sizeErr := term.GetSize(int(os.Stdout.Fd()))
		t.Fatalf("preview requires a terminal at least 80 columns by 16 rows (stdin terminal=%t, input terminal=%t, stdout terminal=%t, size=%dx%d, size error=%v, TERM=%q)", terminalInput(os.Stdin), terminalInput(a.inputFile), terminalInput(os.Stdout), width, height, sizeErr, os.Getenv("TERM"))
	}
	before, err := term.GetState(int(a.inputFile.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	outputBefore, err := term.GetState(int(os.Stdout.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		after, err := term.GetState(int(a.inputFile.Fd()))
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Errorf("terminal input mode was not restored: %v", err)
		}
		outputAfter, err := term.GetState(int(os.Stdout.Fd()))
		if err != nil || !reflect.DeepEqual(outputBefore, outputAfter) {
			t.Errorf("terminal output mode was not restored: %v", err)
		}
	}()
	if err := a.setup(context.Background()); err != nil && !errors.Is(err, errCancelled) {
		t.Fatal(err)
	}
}
