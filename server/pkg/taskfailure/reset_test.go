package taskfailure

import (
	"testing"
	"time"
)

// fixedNow is an arbitrary reference instant for the table tests:
// 2026-06-11 17:09:00 UTC — a Thursday, 18:09 in Europe/London (BST).
// Chosen to mirror the real cap event that motivated ParseResetAt
// ("You've hit your session limit · resets 9:30pm (Europe/London)").
var fixedNow = time.Date(2026, 6, 11, 17, 9, 0, 0, time.UTC)

// TestParseResetAtParseable walks every grammar rule with a real-world
// shaped sample. ParseResetAt is intentionally conservative: each rule
// here is unambiguous given `now` — anything fuzzier belongs in the
// ambiguous table below, not in a new rule.
func TestParseResetAtParseable(t *testing.T) {
	t.Parallel()

	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatalf("load Europe/London: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want time.Time
	}{
		// 1. Explicit RFC3339 reset timestamp.
		{
			"rfc3339 after resets at",
			"Rate limited. resets at 2026-06-12T01:00:00Z",
			time.Date(2026, 6, 12, 1, 0, 0, 0, time.UTC),
		},
		{
			"rfc3339 with offset",
			"usage limit reached — reset at 2026-06-12T09:30:00+01:00",
			time.Date(2026, 6, 12, 8, 30, 0, 0, time.UTC),
		},

		// 2. Explicit durations with units.
		{
			"retry after seconds",
			`API Error: 429 {"type":"rate_limit_error"} retry after 30 seconds`,
			fixedNow.Add(30 * time.Second),
		},
		{
			"retry after minutes",
			"Overloaded, retry after 5 minutes",
			fixedNow.Add(5 * time.Minute),
		},
		{
			"resets in hours",
			"You've hit your usage limit, resets in 2 hours",
			fixedNow.Add(2 * time.Hour),
		},
		{
			"try again in minutes",
			"rate_limit_error: please try again in 6 minutes",
			fixedNow.Add(6 * time.Minute),
		},
		{
			"retry-after header form is seconds",
			"HTTP 429 Too Many Requests\nretry-after: 120",
			fixedNow.Add(120 * time.Second),
		},

		// 3. Claude session-limit clock form: clock time + IANA zone is
		// deterministic given now — the next occurrence of that wall-clock
		// time in that zone.
		{
			"claude session limit later today",
			"You've hit your session limit · resets 9:30pm (Europe/London)",
			time.Date(2026, 6, 11, 21, 30, 0, 0, london),
		},
		{
			"claude session limit rolls to tomorrow",
			// 6pm London has already passed at fixedNow (18:09 London).
			"You've hit your session limit · resets 6pm (Europe/London)",
			time.Date(2026, 6, 12, 18, 0, 0, 0, london),
		},
		{
			"12am normalises to midnight",
			"resets 12:15am (UTC)",
			time.Date(2026, 6, 12, 0, 15, 0, 0, time.UTC),
		},
		{
			"12pm normalises to noon",
			"resets at 12pm (UTC)",
			time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseResetAt(tc.in, fixedNow)
			if !ok {
				t.Fatalf("ParseResetAt(%q) ok=false, want %v", tc.in, tc.want)
			}
			if !got.Equal(tc.want) {
				t.Errorf("ParseResetAt(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseResetAtAmbiguous pins the conservative contract: anything
// without an unambiguous reset instant must return ok=false so the
// caller falls back to the configured default backoff. Loosening a rule
// such that one of these starts parsing is a regression, not progress.
func TestParseResetAtAmbiguous(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"bare 429", "API Error: 429 Too Many Requests"},
		{"hit your limit no time", "you've hit your limit; upgrade to continue"},
		{"resets soon", "quota exceeded, resets soon"},
		{"resets tomorrow", "limit resets tomorrow"},
		{"retry after no amount", "retry after a while"},
		{"retry after amount no unit", "retry after 30"},
		{"clock time without zone", "session limit reached · resets 9:30pm"},
		{"clock time with bad zone", "resets 9pm (Mars/Olympus)"},
		{"clock hour out of range", "resets 25:99pm (Europe/London)"},
		{"duration implausibly large", "retry after 999999 hours"},
		{"unrelated timestamp", "task created at 2026-06-12T01:00:00Z failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, ok := ParseResetAt(tc.in, fixedNow); ok {
				t.Errorf("ParseResetAt(%q) = (%v, true), want ok=false", tc.in, got)
			}
		})
	}
}
