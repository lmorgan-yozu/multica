package taskfailure

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxParsedResetDelay bounds how far in the future a parsed duration may
// land. Provider reset windows are minutes-to-hours (session caps reset
// within a day; weekly caps within 7 days). A duration beyond this is
// almost certainly garbage matched out of unrelated text, and treating it
// as unparseable lets the caller fall back to the configured default
// backoff instead of parking a task for months.
const maxParsedResetDelay = 7 * 24 * time.Hour

// Compiled at package init for the same reason as providerHTTP5xxRe:
// ParseResetAt sits on the failed-task write path.
var (
	// "resets at 2026-06-12T01:00:00Z" / "reset at 2026-06-12T09:30:00+01:00".
	// Anchored to a reset keyword so an unrelated timestamp elsewhere in the
	// error text (log lines, request ids) is never mistaken for a reset time.
	resetTimestampRe = regexp.MustCompile(`(?i)resets?\s+(?:at\s+)?(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))`)

	// "retry after 30 seconds" / "resets in 2 hours" / "try again in 6
	// minutes". The unit is mandatory — a bare "retry after 30" is
	// ambiguous in free text and must not parse.
	resetDurationRe = regexp.MustCompile(`(?i)(?:retry\s+after|resets?\s+in|try\s+again\s+in)\s+(\d{1,6})\s*(seconds?|secs?|minutes?|mins?|hours?|hrs?)\b`)

	// "retry-after: 120" — the HTTP Retry-After header echoed into error
	// text. The header-style colon form is seconds by HTTP spec, so no
	// unit is required here (unlike the free-text form above).
	retryAfterHeaderRe = regexp.MustCompile(`(?i)retry-after:\s*(\d{1,6})\b`)

	// Claude session-limit form: "resets 9:30pm (Europe/London)" /
	// "resets at 12am (UTC)". A wall-clock time alone is ambiguous, but
	// clock + IANA zone is deterministic given now: the next occurrence
	// of that wall-clock time in that zone. The parenthesised zone is
	// mandatory — "resets 9:30pm" without one must not parse.
	resetClockRe = regexp.MustCompile(`(?i)resets?\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*([ap]m)\s*\(([^)]+)\)`)
)

// ParseResetAt extracts a provider cap/rate-limit reset instant from a
// free-form error string. It is deliberately conservative: only formats
// that pin an unambiguous instant (given now) return ok=true; everything
// else returns ok=false and the caller is expected to fall back to a
// configured default backoff. When loosening the grammar, every new rule
// needs a "must not parse" counter-example in reset_test.go.
//
// Recognised forms, in priority order when several appear in one string:
//
//  1. RFC3339 timestamp after a reset keyword: "resets at 2026-06-12T01:00:00Z".
//  2. Explicit duration with a unit: "retry after 30 seconds",
//     "resets in 2 hours", "please try again in 6 minutes".
//  3. HTTP header form: "retry-after: 120" (seconds, per HTTP spec).
//  4. Claude-style wall-clock + IANA zone: "resets 9:30pm (Europe/London)"
//     — resolved to the next occurrence of that wall-clock time in that
//     zone after now.
//
// The returned time may be in the past for form 1 (a stale timestamp in
// the text); callers should clamp to now. Durations are bounded by
// maxParsedResetDelay — anything larger returns ok=false.
func ParseResetAt(rawError string, now time.Time) (time.Time, bool) {
	if strings.TrimSpace(rawError) == "" {
		return time.Time{}, false
	}

	if m := resetTimestampRe.FindStringSubmatch(rawError); m != nil {
		// The regex optionally captures fractional seconds, which RFC3339
		// rejects but RFC3339Nano accepts — try both layouts.
		for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
			if ts, err := time.Parse(layout, m[1]); err == nil {
				return ts, true
			}
		}
		return time.Time{}, false
	}

	if m := resetDurationRe.FindStringSubmatch(rawError); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, false
		}
		var unit time.Duration
		switch strings.ToLower(m[2])[0] {
		case 's':
			unit = time.Second
		case 'm':
			unit = time.Minute
		case 'h':
			unit = time.Hour
		}
		d := time.Duration(n) * unit
		if d <= 0 || d > maxParsedResetDelay {
			return time.Time{}, false
		}
		return now.Add(d), true
	}

	if m := retryAfterHeaderRe.FindStringSubmatch(rawError); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, false
		}
		d := time.Duration(n) * time.Second
		if d <= 0 || d > maxParsedResetDelay {
			return time.Time{}, false
		}
		return now.Add(d), true
	}

	if m := resetClockRe.FindStringSubmatch(rawError); m != nil {
		return parseClockReset(m, now)
	}

	return time.Time{}, false
}

// parseClockReset resolves a resetClockRe match (hour, optional minute,
// am/pm, IANA zone) to the next occurrence of that wall-clock time in
// that zone after now. Unknown zones and out-of-range clock values
// return ok=false.
func parseClockReset(m []string, now time.Time) (time.Time, bool) {
	hour, err := strconv.Atoi(m[1])
	if err != nil || hour < 1 || hour > 12 {
		return time.Time{}, false
	}
	minute := 0
	if m[2] != "" {
		minute, err = strconv.Atoi(m[2])
		if err != nil || minute > 59 {
			return time.Time{}, false
		}
	}
	if strings.EqualFold(m[3], "pm") {
		if hour != 12 {
			hour += 12
		}
	} else if hour == 12 { // 12am → midnight
		hour = 0
	}

	loc, err := time.LoadLocation(strings.TrimSpace(m[4]))
	if err != nil {
		return time.Time{}, false
	}

	nowLocal := now.In(loc)
	candidate := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), hour, minute, 0, 0, loc)
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate, true
}
