package iot

import (
	"net/http"
	"testing"
	"time"
)

// TestParseRetryAfterForms covers both header forms HA (or a proxy in
// front of it) may send: delta-seconds and HTTP-date.
func TestParseRetryAfterForms(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"2", 2 * time.Second},
		{"0", 0},
		{"garbage", 0},
		{"-1", 0},
	}
	for _, tc := range cases {
		if got := parseRetryAfter(tc.in); got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	future := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got < 2*time.Second || got > 4*time.Second {
		t.Errorf("parseRetryAfter(future) = %v, want ~3s", got)
	}

	past := time.Now().Add(-3 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("parseRetryAfter(past) = %v, want 0", got)
	}
}

func TestHABackoffSchedule(t *testing.T) {
	if got := haBackoff(1); got != 250*time.Millisecond {
		t.Errorf("haBackoff(1) = %v, want 250ms", got)
	}
	if got := haBackoff(2); got != 500*time.Millisecond {
		t.Errorf("haBackoff(2) = %v, want 500ms", got)
	}
}
