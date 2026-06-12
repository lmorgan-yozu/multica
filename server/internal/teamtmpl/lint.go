package teamtmpl

import (
	"fmt"
	"regexp"
	"strings"
)

// Finding is one lint hit. Export fails closed on any finding: the
// exporter fixes the source material or excludes the artefact — there is
// deliberately no override flag (ADA-30 AC 5/6: templates must never
// carry autonomy grants or secrets, so the gate is not negotiable at the
// API).
type Finding struct {
	// Location is the human-addressable place the marker was found
	// (e.g. `role "tech-lead" instructions`).
	Location string `json:"location"`
	// Rule names which lint fired: "autonomy_language", "blocked_skill",
	// or "secret_pattern".
	Rule string `json:"rule"`
	// Marker is the matched phrase/pattern name.
	Marker string `json:"marker"`
	// Excerpt is a short window around the match for locating it. For
	// secret_pattern findings the matched value itself is REDACTED — a
	// lint report must not become the leak it exists to prevent.
	Excerpt string `json:"excerpt"`
}

// autonomyMarkers are case-insensitive phrases that indicate the ADA-1
// autonomous-mode exception (or equivalent standing-authority language)
// is being carried into a template. The list is intentionally specific:
// templates describe how a team works, so generic words like "autonomy"
// would drown the signal in false positives.
var autonomyMarkers = []string{
	"autonomous mode",
	"ada-1",
	"standing authorisation",
	"standing authorization",
	"standing build authorisation",
	"standing build authorization",
	"without per-issue human sign-off",
	"no human sign-off",
	"no per-issue human sign-off",
	"do not wait on him",
	"do not assign issues to the human for approval",
}

// blockedSkillNames are skills that must never leave their home
// workspace, whatever their content happens to say. ada-house-rules is
// the ADA-1 autonomy grant itself.
var blockedSkillNames = map[string]bool{
	"ada-house-rules": true,
}

// secretPatterns catch well-known credential shapes. This is defence in
// depth behind the structural whitelist (the manifest schema has no
// secret-bearing field) — it exists for secrets pasted into instructions
// or skill bodies by hand.
var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"github_token", regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`)},
	{"github_fine_grained_pat", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`)},
	{"openai_key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"anthropic_key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)},
	{"aws_access_key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"slack_token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"private_key_block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"bearer_header", regexp.MustCompile(`(?i)authorization:\s*bearer\s+[A-Za-z0-9._-]{16,}`)},
}

const excerptRadius = 40

// Lint scans every text location in the manifest for autonomy language
// and secret patterns, and rejects blocked skills by name. A nil/empty
// result means the manifest is exportable.
func Lint(m *Manifest) []Finding {
	var findings []Finding

	for _, s := range m.Skills {
		if blockedSkillNames[strings.ToLower(s.Name)] {
			findings = append(findings, Finding{
				Location: fmt.Sprintf("skill %q", s.Name),
				Rule:     "blocked_skill",
				Marker:   s.Name,
				Excerpt:  "this skill is workspace-scoped and can never be exported",
			})
		}
	}

	for _, loc := range m.textLocations() {
		lower := strings.ToLower(loc.text)
		for _, marker := range autonomyMarkers {
			if idx := strings.Index(lower, marker); idx >= 0 {
				findings = append(findings, Finding{
					Location: loc.where,
					Rule:     "autonomy_language",
					Marker:   marker,
					Excerpt:  excerpt(loc.text, idx, len(marker)),
				})
			}
		}
		for _, sp := range secretPatterns {
			if sp.re.MatchString(loc.text) {
				findings = append(findings, Finding{
					Location: loc.where,
					Rule:     "secret_pattern",
					Marker:   sp.name,
					Excerpt:  "[REDACTED]",
				})
			}
		}
	}

	return findings
}

func excerpt(text string, idx, matchLen int) string {
	start := idx - excerptRadius
	if start < 0 {
		start = 0
	}
	end := idx + matchLen + excerptRadius
	if end > len(text) {
		end = len(text)
	}
	out := strings.ReplaceAll(text[start:end], "\n", " ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(text) {
		out += "…"
	}
	return out
}
