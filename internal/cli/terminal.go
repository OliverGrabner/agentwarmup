package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

type choice struct{ value, label string }

func terminalInput(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func (a *app) configureTerminal(f *os.File) {
	a.inputFile = f
	out, ok := a.out.(*os.File)
	if !ok || !terminalInput(f) || !terminalInput(out) || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return
	}
	width, height, err := term.GetSize(int(out.Fd()))
	if err != nil || width < 80 || height < 16 {
		return
	}
	a.startRaw = func() (func() error, error) {
		width, height, err := term.GetSize(int(out.Fd()))
		if err != nil || width < 80 || height < 16 {
			return nil, errors.New("terminal is too small for selection menus")
		}
		restoreOutput, err := terminalOutput(out)
		if err != nil {
			return nil, err
		}
		state, err := term.MakeRaw(int(f.Fd()))
		if err != nil {
			_ = restoreOutput()
			return nil, err
		}
		return func() error { return errors.Join(term.Restore(int(f.Fd()), state), restoreOutput()) }, nil
	}
}

func (a *app) closeTerminal() {
	if a.closeInput && a.inputFile != nil {
		_ = a.inputFile.Close()
	}
}

// Raw mode is scoped to one prompt, including all error and cancellation paths.
// The normal screen and cursor stay visible. Monochrome output also honors NO_COLOR.
func (a *app) rawPrompt(fn func() error) (err error) {
	restore, err := a.startRaw()
	if err != nil {
		a.startRaw = nil
		return err
	}
	defer func() { err = errors.Join(err, restore()) }()
	return fn()
}

func (a *app) readByte(timeout time.Duration) (byte, error) {
	type result struct {
		b   byte
		err error
	}
	done := make(chan result, 1)
	go func() { b, err := a.in.ReadByte(); done <- result{b, err} }()
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case <-ctx.Done():
		return 0, errCancelled
	case <-deadline:
		return 0, errCancelled
	case r := <-done:
		if r.err != nil {
			return 0, errCancelled
		}
		return r.b, nil
	}
}

const (
	keyUp   byte = 0x80
	keyDown byte = 0x81
)

func (a *app) key() (byte, error) {
	b, err := a.readByte(0)
	if err != nil {
		return 0, err
	}
	switch b {
	case 3, 4, 26:
		return 0, errCancelled
	case 27:
		// A lone Escape cancels. CSI and SS3 arrows work in common terminals.
		prefix, err := a.readByte(150 * time.Millisecond)
		if err != nil || (prefix != '[' && prefix != 'O') {
			return 0, errCancelled
		}
		code, err := a.readByte(150 * time.Millisecond)
		if err != nil {
			return 0, errCancelled
		}
		switch code {
		case 'A':
			return keyUp, nil
		case 'B':
			return keyDown, nil
		}
		return 0, nil
	}
	return b, nil
}

func selectedIndex(options []choice, def string) int {
	for i, option := range options {
		if option.value == def {
			return i
		}
	}
	return 0
}

func (a *app) choose(question string, options []choice, def string, allowText bool) (string, error) {
	if err := a.input(); err != nil {
		return "", err
	}
	if a.startRaw == nil {
		return a.choosePlain(question, options, def, allowText)
	}
	index := selectedIndex(options, def)
	err := a.rawPrompt(func() error {
		fmt.Fprintf(a.out, "\r\n%s\r\nUp/Down to choose, Enter to continue, Esc to cancel.\r\n", question)
		for {
			for i, option := range options {
				marker := " "
				if i == index {
					marker = ">"
				}
				fmt.Fprintf(a.out, "\r\x1b[2K%s %s\r\n", marker, option.label)
			}
			key, err := a.key()
			if err != nil {
				return err
			}
			switch key {
			case '\r', '\n':
				fmt.Fprintf(a.out, "\x1b[%dA\r\x1b[J%s: %s\r\n", len(options)+2, strings.TrimSuffix(question, "?"), options[index].label)
				return nil
			case keyUp, 'k':
				index = (index + len(options) - 1) % len(options)
			case keyDown, 'j':
				index = (index + 1) % len(options)
			default:
				if key >= '1' && int(key-'1') < len(options) {
					index = int(key - '1')
				}
			}
			fmt.Fprintf(a.out, "\x1b[%dA", len(options))
		}
	})
	if err != nil && a.startRaw == nil {
		return a.choosePlain(question, options, def, allowText)
	}
	if err != nil {
		return "", err
	}
	return options[index].value, nil
}

func (a *app) choosePlain(question string, options []choice, def string, allowText bool) (string, error) {
	fmt.Fprintln(a.out, "\n"+question)
	for i, option := range options {
		fmt.Fprintf(a.out, "  %d. %s\n", i+1, option.label)
	}
	for {
		answer, err := a.ask("Choose a number or type a value:", def)
		if err != nil {
			return "", err
		}
		if n, err := strconv.Atoi(answer); err == nil && n > 0 && n <= len(options) {
			return options[n-1].value, nil
		}
		for _, option := range options {
			if strings.EqualFold(answer, option.value) || strings.EqualFold(answer, option.label) {
				return option.value, nil
			}
		}
		if allowText {
			return answer, nil
		}
		fmt.Fprintln(a.out, "Choose one of the listed options.")
	}
}

func (a *app) askTerminal(question, def string) (answer string, err error) {
	err = a.rawPrompt(func() error {
		prompt := question
		if def != "" {
			prompt += " [" + def + "]"
		}
		fmt.Fprint(a.out, prompt+" ")
		var line []byte
		defer fmt.Fprint(a.out, "\r\n")
		for {
			key, err := a.key()
			if err != nil {
				return err
			}
			switch key {
			case '\r', '\n':
				answer = strings.TrimSpace(string(line))
				if answer == "" {
					answer = def
				}
				return nil
			case 8, 127:
				if len(line) > 0 {
					line = line[:len(line)-1]
					fmt.Fprint(a.out, "\b \b")
				}
			default:
				if key >= 32 && key < 127 && len(line) < 128 {
					line = append(line, key)
					fmt.Fprintf(a.out, "%c", key)
				}
			}
		}
	})
	if err != nil && a.startRaw == nil {
		return a.ask(question, def)
	}
	return
}
