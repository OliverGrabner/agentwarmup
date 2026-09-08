package schedule

import (
	"fmt"
	"strconv"
	"strings"
)

// allDays is the canonical ordered week: Sun,Mon,Tue,Wed,Thu,Fri,Sat
var allDays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// LeadHours is how far before the reset the warmup fires.
const LeadHours = 5

// dayIndex returns the position of a canonical day abbreviation in allDays,
// or -1 if it is not one.
func dayIndex(day string) int {
	for i, d := range allDays {
		if d == day {
			return i
		}
	}
	return -1
}

// splitTime parses a normalized "HH:MM" 24-hour string into hour and minute.
func splitTime(t string) (hour, minute int, err error) {
	parts := strings.Split(t, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM")
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("invalid hour")
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid minute")
	}
	return hour, minute, nil
}

// ParseTime validates and normalizes a user-entered time. Accept "10:00",
// "9:30", "10:00 AM", "5pm", "05:00". Return normalized 24-hour "HH:MM".
func ParseTime(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", fmt.Errorf("empty time")
	}

	upper := strings.ToUpper(s)
	hasAM := strings.HasSuffix(upper, "AM")
	hasPM := strings.HasSuffix(upper, "PM")

	numeric := s
	if hasAM || hasPM {
		numeric = strings.TrimSpace(upper[:len(upper)-2])
	}

	var hour, minute int
	var err error
	if strings.Contains(numeric, ":") {
		parts := strings.SplitN(numeric, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid time %q", input)
		}
		hour, err = strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return "", fmt.Errorf("invalid time %q", input)
		}
		minute, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return "", fmt.Errorf("invalid time %q", input)
		}
	} else {
		hour, err = strconv.Atoi(numeric)
		if err != nil {
			return "", fmt.Errorf("invalid time %q", input)
		}
		minute = 0
	}

	if minute < 0 || minute > 59 {
		return "", fmt.Errorf("invalid minute in %q", input)
	}

	if hasAM || hasPM {
		if hour < 1 || hour > 12 {
			return "", fmt.Errorf("invalid hour in %q", input)
		}
		if hasAM {
			if hour == 12 {
				hour = 0
			}
		} else {
			if hour != 12 {
				hour += 12
			}
		}
	} else {
		if hour < 0 || hour > 23 {
			return "", fmt.Errorf("invalid hour in %q", input)
		}
	}

	return fmt.Sprintf("%02d:%02d", hour, minute), nil
}

// ParseDays parses a user's day selection into canonical abbreviations in
// allDays order. Accept "weekdays", "everyday"/"every day"/"daily", "weekends",
// and comma/space lists like "mon,wed,fri" or "Monday Tuesday". Case-insensitive.
// Reject an empty selection with an error.
func ParseDays(input string) ([]string, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return nil, fmt.Errorf("no days selected")
	}

	switch s {
	case "weekdays":
		return []string{"Mon", "Tue", "Wed", "Thu", "Fri"}, nil
	case "everyday", "every day", "daily":
		out := make([]string, len(allDays))
		copy(out, allDays)
		return out, nil
	case "weekends":
		return []string{"Sun", "Sat"}, nil
	}

	// comma and/or space separated list of day names/abbreviations
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return nil, fmt.Errorf("no days selected")
	}

	names := map[string]string{
		"sun": "Sun", "sunday": "Sun",
		"mon": "Mon", "monday": "Mon",
		"tue": "Tue", "tues": "Tue", "tuesday": "Tue",
		"wed": "Wed", "wednesday": "Wed",
		"thu": "Thu", "thur": "Thu", "thurs": "Thu", "thursday": "Thu",
		"fri": "Fri", "friday": "Fri",
		"sat": "Sat", "saturday": "Sat",
	}

	selected := map[string]bool{}
	for _, f := range fields {
		canon, ok := names[f]
		if !ok {
			return nil, fmt.Errorf("unrecognized day %q", f)
		}
		selected[canon] = true
	}

	if len(selected) == 0 {
		return nil, fmt.Errorf("no days selected")
	}

	out := make([]string, 0, len(selected))
	for _, d := range allDays {
		if selected[d] {
			out = append(out, d)
		}
	}
	return out, nil
}

// FormatDays renders days for display: "Monday-Friday" for exactly the five
// weekdays, "every day" for all seven, otherwise a comma list like "Mon, Wed, Fri".
func FormatDays(days []string) string {
	if len(days) == 7 {
		return "every day"
	}
	if len(days) == 5 {
		weekdays := map[string]bool{"Mon": true, "Tue": true, "Wed": true, "Thu": true, "Fri": true}
		allWeekdays := true
		for _, d := range days {
			if !weekdays[d] {
				allWeekdays = false
				break
			}
		}
		if allWeekdays {
			return "Monday-Friday"
		}
	}
	return strings.Join(days, ", ")
}
