package cli

import (
	"testing"
	"time"
)

func TestParsePeriodAcceptsDaysAndDurations(t *testing.T) {
	valid := map[string]time.Duration{
		"30d":  30 * 24 * time.Hour,
		" 7d ": 7 * 24 * time.Hour,
		"12h":  12 * time.Hour,
		"90m":  90 * time.Minute,
	}
	for input, want := range valid {
		got, err := parsePeriod(input)
		if err != nil || got != want {
			t.Errorf("parsePeriod(%q) = %v, %v; want %v", input, got, err, want)
		}
	}

	for _, input := range []string{"", "d", "0d", "-1d", "1.5d", "abc", "0s", "-2h"} {
		if _, err := parsePeriod(input); err == nil {
			t.Errorf("parsePeriod(%q) succeeded, want an error", input)
		}
	}
}
