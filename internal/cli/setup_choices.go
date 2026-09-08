package cli

import (
	"fmt"
	"strings"

	"github.com/OliverGrabner/agentwarmup/internal/schedule"
)

func timeChoices(reset string) []choice {
	options := []choice{{"09:00", "9:00 AM"}, {"10:00", "10:00 AM"}, {"11:00", "11:00 AM"}, {"12:00", "12:00 PM"}}
	found := false
	for _, option := range options {
		found = found || option.value == reset
	}
	if !found {
		options = append([]choice{{reset, clock12(reset) + " (current)"}}, options...)
	}
	return append(options, choice{"custom", "Custom time..."})
}

func (a *app) resetTime(reset string) (string, error) {
	for {
		answer, err := a.choose("Target reset", timeChoices(reset), reset, true)
		if err != nil {
			return "", err
		}
		if answer == "custom" {
			answer, err = a.ask("Target reset (for example 9:30 AM):", clock12(reset))
			if err != nil {
				return "", err
			}
		}
		if parsed, err := schedule.ParseTime(answer); err == nil {
			return parsed, nil
		}
		fmt.Fprintln(a.out, "Use a time such as 10:00 AM or 17:00.")
	}
}

func dayChoices(days []string) []choice {
	options := []choice{{"weekdays", "Weekdays"}, {"everyday", "Every day"}}
	def := dayDefault(days)
	if def != "weekdays" && def != "everyday" {
		options = append(options, choice{def, schedule.FormatDays(days) + " (current)"})
	}
	return append(options, choice{"custom", "Choose days..."})
}

func (a *app) resetDays(days []string) ([]string, error) {
	for {
		answer, err := a.choose("Reset days", dayChoices(days), dayDefault(days), true)
		if err != nil {
			return nil, err
		}
		if answer == "custom" {
			if a.startRaw != nil {
				return a.pickDays(days)
			}
			answer, err = a.ask("Days (for example mon,wed,fri):", dayDefault(days))
			if err != nil {
				return nil, err
			}
		}
		if parsed, err := schedule.ParseDays(answer); err == nil {
			return parsed, nil
		}
		fmt.Fprintln(a.out, "Use weekdays, everyday, weekends, or mon,wed,fri.")
	}
}

func (a *app) pickDays(previous []string) ([]string, error) {
	days := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	selected := make([]bool, len(days))
	for i, day := range days {
		for _, old := range previous {
			selected[i] = selected[i] || old == day
		}
	}
	index := 0
	var result []string
	err := a.rawPrompt(func() error {
		fmt.Fprint(a.out, "\r\nChoose reset days\r\nUp/Down to move, Space to toggle, Enter to continue, Esc to cancel.\r\n")
		message := ""
		for {
			for i, day := range days {
				marker, check := " ", " "
				if index == i {
					marker = ">"
				}
				if selected[i] {
					check = "x"
				}
				fmt.Fprintf(a.out, "\r\x1b[2K%s [%s] %s\r\n", marker, check, day)
			}
			fmt.Fprintf(a.out, "\r\x1b[2K%s\r\n", message)
			key, err := a.key()
			if err != nil {
				return err
			}
			switch key {
			case keyUp, 'k':
				index = (index + len(days) - 1) % len(days)
			case keyDown, 'j':
				index = (index + 1) % len(days)
			case ' ':
				selected[index] = !selected[index]
			case '\r', '\n':
				result = nil
				for i, day := range days {
					if selected[i] {
						result = append(result, day)
					}
				}
				if len(result) > 0 {
					fmt.Fprintf(a.out, "\x1b[%dA\r\x1b[JReset days: %s\r\n", len(days)+3, schedule.FormatDays(result))
					return nil
				}
				message = "Select at least one day."
			}
			fmt.Fprintf(a.out, "\x1b[%dA", len(days)+1)
		}
	})
	if err != nil {
		if a.startRaw == nil {
			return a.resetDays(previous)
		}
		return nil, err
	}
	return schedule.ParseDays(strings.Join(result, ","))
}

func providerLabels(providers []string) string {
	names := make([]string, len(providers))
	for i, id := range providers {
		names[i] = label(id)
	}
	return strings.Join(names, ", ")
}
