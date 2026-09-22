package llm

import (
	"testing"
	"time"
)

func TestLimitsFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name, timeout, tokens string
		want                  Limits
		invalid               bool
	}{
		{"defaults", "", "", Limits{180 * time.Second, 8000}, false},
		{"override", "5m", "12000", Limits{5 * time.Minute, 12000}, false},
		{"no-unit", "180", "", Limits{}, true},
		{"zero-timeout", "0s", "", Limits{}, true},
		{"negative-timeout", "-1s", "", Limits{}, true},
		{"overflow-margin", "2562047h47m16s", "", Limits{}, true},
		{"zero-tokens", "", "0", Limits{}, true},
		{"negative-tokens", "", "-5", Limits{}, true},
		{"fractional-tokens", "", "8000.5", Limits{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENROUTER_TIMEOUT", tc.timeout)
			t.Setenv("OPENROUTER_MAX_TOKENS", tc.tokens)
			got, err := LimitsFromEnv()
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.invalid && got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
