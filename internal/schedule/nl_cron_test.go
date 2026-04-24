package schedule_test

import (
	"testing"

	"github.com/LumabyteCo/aibutler/internal/schedule"
)

func TestNLToCronBasicPatterns(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"every minute", "*/5 * * * *"},
		{"every hour", "0 * * * *"},
		{"hourly", "0 * * * *"},
		{"every day", "0 9 * * *"},
		{"daily", "0 9 * * *"},
		{"every morning", "0 7 * * *"},
		{"every evening", "0 18 * * *"},
		{"every night", "0 21 * * *"},
		{"every week", "0 9 * * 1"},
		{"weekly", "0 9 * * 1"},
		{"every month", "0 9 1 * *"},
		{"monthly", "0 9 1 * *"},
		{"every weekday", "0 9 * * 1-5"},
		{"every weekend", "0 10 * * 0,6"},
	}

	for _, tc := range tests {
		got, err := schedule.NLToCron(tc.input)
		if err != nil {
			t.Errorf("NLToCron(%q): %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NLToCron(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNLToCronWithTime(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"every day at 14:30", "30 14 * * *"},
		{"every day at 08:00", "0 8 * * *"},
		{"every monday at 10:00", "0 10 * * 1"},
		{"every friday at 17:30", "30 17 * * 5"},
	}

	for _, tc := range tests {
		got, err := schedule.NLToCron(tc.input)
		if err != nil {
			t.Errorf("NLToCron(%q): %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NLToCron(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNLToCronInvalid(t *testing.T) {
	_, err := schedule.NLToCron("something random")
	if err == nil {
		t.Error("expected error for unparseable input")
	}
}

func TestNLToCronCaseInsensitive(t *testing.T) {
	got, err := schedule.NLToCron("Every Day At 09:00")
	if err != nil {
		t.Fatalf("NLToCron: %v", err)
	}
	if got != "0 9 * * *" {
		t.Errorf("got = %q, want '0 9 * * *'", got)
	}
}
