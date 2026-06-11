package service

import (
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestParseSessionCapResilience(t *testing.T) {
	t.Parallel()

	defaults := sessionCapResilienceSettings{
		Enabled:               true,
		DefaultBackoffMinutes: 60,
		MaxResumeAttempts:     3,
	}

	cases := []struct {
		name string
		raw  string
		want sessionCapResilienceSettings
	}{
		{"empty settings", "", defaults},
		{"no key", `{"github_enabled": true}`, defaults},
		{"malformed json", `{not json`, defaults},
		{"key wrong type", `{"session_cap_resilience": "yes"}`, defaults},
		{
			"explicitly disabled",
			`{"session_cap_resilience": {"enabled": false}}`,
			sessionCapResilienceSettings{Enabled: false, DefaultBackoffMinutes: 60, MaxResumeAttempts: 3},
		},
		{
			"full override",
			`{"session_cap_resilience": {"enabled": true, "default_backoff_minutes": 15, "max_resume_attempts": 5}}`,
			sessionCapResilienceSettings{Enabled: true, DefaultBackoffMinutes: 15, MaxResumeAttempts: 5},
		},
		{
			"zero backoff falls back to default",
			`{"session_cap_resilience": {"default_backoff_minutes": 0}}`,
			defaults,
		},
		{
			"negative backoff falls back to default",
			`{"session_cap_resilience": {"default_backoff_minutes": -10}}`,
			defaults,
		},
		{
			"zero max attempts is respected",
			`{"session_cap_resilience": {"max_resume_attempts": 0}}`,
			sessionCapResilienceSettings{Enabled: true, DefaultBackoffMinutes: 60, MaxResumeAttempts: 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseSessionCapResilience([]byte(tc.raw)); got != tc.want {
				t.Errorf("parseSessionCapResilience(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestIsCapFailureReason(t *testing.T) {
	t.Parallel()

	capReasons := []string{
		taskfailure.ReasonAgentProviderQuotaLimit.String(),
		taskfailure.ReasonAgentProviderCapacityOrRateLimit.String(),
	}
	for _, r := range capReasons {
		if !isCapFailureReason(r) {
			t.Errorf("isCapFailureReason(%q) = false, want true", r)
		}
	}

	// Every other canonical reason — plus legacy/empty strings — must be
	// outside the cap class (AC4: non-cap failures byte-for-byte unaffected).
	for _, r := range taskfailure.AllReasons() {
		if r == taskfailure.ReasonAgentProviderQuotaLimit || r == taskfailure.ReasonAgentProviderCapacityOrRateLimit {
			continue
		}
		if isCapFailureReason(r.String()) {
			t.Errorf("isCapFailureReason(%q) = true, want false", r)
		}
	}
	for _, r := range []string{"", "agent_error", "local_directory_error"} {
		if isCapFailureReason(r) {
			t.Errorf("isCapFailureReason(%q) = true, want false", r)
		}
	}
}

func TestCapResumeTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 11, 17, 9, 0, 0, time.UTC)
	cfg := sessionCapResilienceSettings{Enabled: true, DefaultBackoffMinutes: 45, MaxResumeAttempts: 3}

	t.Run("unparseable error uses configured backoff", func(t *testing.T) {
		t.Parallel()
		got, parsed := capResumeTime("API Error: 429 Too Many Requests", now, cfg)
		if parsed {
			t.Fatal("parsed = true, want false")
		}
		if want := now.Add(45 * time.Minute); !got.Equal(want) {
			t.Errorf("resume = %v, want %v", got, want)
		}
	})

	t.Run("parseable future reset is used verbatim", func(t *testing.T) {
		t.Parallel()
		got, parsed := capResumeTime("usage limit reached, resets at 2026-06-11T21:30:00Z", now, cfg)
		if !parsed {
			t.Fatal("parsed = false, want true")
		}
		want := time.Date(2026, 6, 11, 21, 30, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("resume = %v, want %v", got, want)
		}
		// AC2: never earlier than the provider reset.
		if got.Before(want) {
			t.Errorf("resume %v is before provider reset %v", got, want)
		}
	})

	t.Run("stale past reset clamps to now", func(t *testing.T) {
		t.Parallel()
		got, parsed := capResumeTime("resets at 2026-06-11T01:00:00Z", now, cfg)
		if !parsed {
			t.Fatal("parsed = false, want true")
		}
		if !got.Equal(now) {
			t.Errorf("resume = %v, want clamp to now %v", got, now)
		}
	})

	t.Run("real cap message from ADA-44", func(t *testing.T) {
		t.Parallel()
		got, parsed := capResumeTime("You've hit your session limit · resets 9:30pm (Europe/London)", now, cfg)
		if !parsed {
			t.Fatal("parsed = false, want true")
		}
		london, err := time.LoadLocation("Europe/London")
		if err != nil {
			t.Fatalf("load zone: %v", err)
		}
		want := time.Date(2026, 6, 11, 21, 30, 0, 0, london)
		if !got.Equal(want) {
			t.Errorf("resume = %v, want %v", got, want)
		}
	})
}
