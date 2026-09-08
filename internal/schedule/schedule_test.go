package schedule

import (
	"reflect"
	"testing"
)

func TestParseTime(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "10:00", want: "10:00"},
		{input: "9:30", want: "09:30"},
		{input: "10:00 AM", want: "10:00"},
		{input: "10:00AM", want: "10:00"},
		{input: "5pm", want: "17:00"},
		{input: "5PM", want: "17:00"},
		{input: "05:00", want: "05:00"},
		{input: "12:00 AM", want: "00:00"},
		{input: "12:00 PM", want: "12:00"},
		{input: "12am", want: "00:00"},
		{input: "12pm", want: "12:00"},
		{input: "  9:05 am  ", want: "09:05"},
		{input: "25:00", wantErr: true},
		{input: "abc", wantErr: true},
		{input: "", wantErr: true},
		{input: "13:00 PM", wantErr: true},
		{input: "0:60", wantErr: true},
		{input: "-1:00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseTime(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseTime(%q) expected error, got %q", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTime(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseTime(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseDays(t *testing.T) {
	weekdays := []string{"Mon", "Tue", "Wed", "Thu", "Fri"}
	weekends := []string{"Sun", "Sat"}

	tests := []struct {
		input   string
		want    []string
		wantErr bool
	}{
		{input: "weekdays", want: weekdays},
		{input: "Weekdays", want: weekdays},
		{input: "everyday", want: allDays},
		{input: "every day", want: allDays},
		{input: "daily", want: allDays},
		{input: "weekends", want: weekends},
		{input: "mon,wed,fri", want: []string{"Mon", "Wed", "Fri"}},
		{input: "Monday Tuesday", want: []string{"Mon", "Tue"}},
		{input: "SAT, sun", want: []string{"Sun", "Sat"}},
		{input: "fri, mon", want: []string{"Mon", "Fri"}},
		{input: "", wantErr: true},
		{input: "   ", wantErr: true},
		{input: "garbage", wantErr: true},
		{input: "mon, blorp", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseDays(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseDays(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDays(%q) unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseDays(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatDays(t *testing.T) {
	tests := []struct {
		name string
		days []string
		want string
	}{
		{name: "weekdays", days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}, want: "Monday-Friday"},
		{name: "everyday", days: allDays, want: "every day"},
		{name: "custom list", days: []string{"Mon", "Wed", "Fri"}, want: "Mon, Wed, Fri"},
		{name: "weekends", days: []string{"Sat", "Sun"}, want: "Sat, Sun"},
		{name: "single day", days: []string{"Mon"}, want: "Mon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDays(tt.days)
			if got != tt.want {
				t.Errorf("FormatDays(%v) = %q, want %q", tt.days, got, tt.want)
			}
		})
	}
}
