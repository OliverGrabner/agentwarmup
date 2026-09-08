package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/OliverGrabner/agentwarmup/internal/config"
	"github.com/OliverGrabner/agentwarmup/internal/install"
	"github.com/OliverGrabner/agentwarmup/internal/job"
	"github.com/OliverGrabner/agentwarmup/internal/provider"
	"github.com/OliverGrabner/agentwarmup/internal/runner"
)

// Version is set by the release build.
var Version = "dev"

// Preview remains true unless a release build has passed the validation gate.
var Preview = "true"

var errCancelled = errors.New("cancelled; no schedule changes saved")
var errNotSetUp = errors.New("not set up yet; run agentwarmup setup")

type scheduler interface {
	Check() error
	Install(string) error
	Inspect() (job.Status, error)
	SetEnabled(bool) error
	Uninstall() error
}

// app keeps interactive input and side effects replaceable in lifecycle tests.
type app struct {
	ctx                           context.Context
	paths                         config.Paths
	in                            *bufio.Reader
	out                           io.Writer
	now                           func() time.Time
	jobs                          scheduler
	detect                        func(context.Context, string) (provider.Installation, error)
	verify                        func(context.Context, provider.Installation) (provider.Installation, error)
	auth                          func(context.Context, provider.Installation) (provider.AuthStatus, error)
	login                         func(context.Context, provider.Installation) error
	ensure                        func(config.Paths) (string, error)
	commit, rollback, removeFiles func(config.Paths) error
	zone                          func() (string, error)
	startRaw                      func() (func() error, error)
	inputFile                     *os.File
	closeInput                    bool
}

func newApp(paths config.Paths) *app {
	return &app{paths: paths, out: os.Stdout, now: time.Now, jobs: job.New(paths.Root),
		detect: provider.Detect, verify: provider.Verify, auth: provider.CheckAuth, login: provider.Login,
		ensure: install.Ensure, commit: install.Commit, rollback: install.Rollback, removeFiles: install.Uninstall,
		zone: detectZone}
}

func Run(args []string) error {
	command, home, err := arguments(args)
	if err != nil {
		return err
	}
	if command == "help" {
		usage(os.Stdout)
		return nil
	}
	if command == "version" {
		fmt.Fprintln(os.Stdout, "agentwarmup", Version)
		return nil
	}
	var paths config.Paths
	if home != "" {
		paths, err = config.ResolvePaths(home)
	} else {
		paths, err = config.DefaultPaths()
	}
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a := newApp(paths)
	defer a.closeTerminal()
	a.ctx = ctx
	switch command {
	case "":
		if _, err := config.Load(paths.Config()); os.IsNotExist(err) {
			return a.setup(ctx)
		} else if err != nil {
			return repair(err)
		}
		return a.status()
	case "setup":
		return a.setup(ctx)
	case "status":
		return a.status()
	case "pause":
		return a.enable(ctx, false)
	case "resume":
		return a.enable(ctx, true)
	case "uninstall":
		return a.uninstall(ctx)
	case "install":
		return a.update(ctx)
	case "check", "warmup":
		return runner.New(paths).Check(ctx)
	default:
		return fmt.Errorf("unknown command %q; run agentwarmup --help", command)
	}
}

func arguments(args []string) (command, home string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--home" {
			if home != "" || i+1 >= len(args) || args[i+1] == "" {
				return "", "", errors.New("--home requires one application directory")
			}
			i++
			home = args[i]
			continue
		}
		if command != "" {
			return "", "", fmt.Errorf("unexpected argument %q", arg)
		}
		command = arg
	}
	switch command {
	case "--help", "-h":
		command = "help"
	case "--version", "-v":
		command = "version"
	}
	return
}

func usage(out io.Writer) {
	fmt.Fprint(out, `AgentWarmup — schedule an early request before your workday.

  agentwarmup            status, or setup on first run
  agentwarmup setup      choose providers, reset time, and days
  agentwarmup pause      pause future warmups
  agentwarmup resume     resume your schedule
  agentwarmup uninstall  remove AgentWarmup
`)
}

func repair(err error) error {
	return fmt.Errorf("cannot read settings: %w; repair config.json before continuing", err)
}

func (a *app) input() error {
	if a.in != nil {
		return nil
	}
	if terminalInput(os.Stdin) {
		a.in = bufio.NewReader(os.Stdin)
		a.configureTerminal(os.Stdin)
		return nil
	}
	// A piped installer must read answers from the terminal, never its script.
	terminal := "/dev/tty"
	if runtime.GOOS == "windows" {
		terminal = "CONIN$"
	}
	f, err := os.Open(terminal)
	if err != nil {
		return errors.New("setup needs an interactive terminal; run agentwarmup setup there")
	}
	a.in = bufio.NewReader(f)
	a.closeInput = true
	a.configureTerminal(f)
	return nil
}

func (a *app) ask(question, def string) (string, error) {
	if err := a.input(); err != nil {
		return "", err
	}
	if a.startRaw != nil {
		return a.askTerminal(question, def)
	}
	fmt.Fprintf(a.out, "%s", question)
	if def != "" {
		fmt.Fprintf(a.out, " [%s]", def)
	}
	fmt.Fprint(a.out, " ")
	type answer struct {
		line string
		err  error
	}
	done := make(chan answer, 1)
	go func() { line, err := a.in.ReadString('\n'); done <- answer{line, err} }()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var line string
	var err error
	select {
	case result := <-done:
		line, err = result.line, result.err
	case <-ctx.Done():
		return "", errCancelled
	}
	// EOF also cancels partial input; it must never enable schedule defaults.
	if err != nil {
		return "", errCancelled
	}
	line = strings.TrimSpace(line)
	if strings.ContainsAny(line, "\x03\x04\x1b\x1a") {
		return "", errCancelled
	}
	if line == "" {
		line = def
	}
	return line, nil
}

func (a *app) confirm(question string, defaultYes bool) (bool, error) {
	if err := a.input(); err != nil {
		return false, err
	}
	if a.startRaw != nil {
		def := "no"
		if defaultYes {
			def = "yes"
		}
		answer, err := a.choose(question, []choice{{"yes", "Yes"}, {"no", "No"}}, def, false)
		return answer == "yes", err
	}
	def := "y/N"
	if defaultYes {
		def = "Y/n"
	}
	for {
		answer, err := a.ask(question, def)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "y/n":
			return defaultYes, nil
		default:
			fmt.Fprintln(a.out, "Enter yes or no.")
		}
	}
}

func (a *app) load() (*config.Config, error) {
	doc, err := config.Load(a.paths.Config())
	if os.IsNotExist(err) {
		return nil, errNotSetUp
	}
	if err != nil {
		return nil, repair(err)
	}
	if doc.Current == nil {
		return nil, errors.New("settings need updating; run agentwarmup setup")
	}
	return doc.Current, nil
}
