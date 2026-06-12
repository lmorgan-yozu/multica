// Package teamtmpl defines the team-template manifest format and the
// sanitisation rules that guard it. A manifest is a self-contained,
// format-versioned snapshot of a delivery team — role agents, embedded
// skills, squad shape, workflow definitions, autopilot definitions —
// exported from a live workspace and applied to a new engagement.
//
// Design rules (docs/adr/0002-team-templates.md):
//   - Whitelist construction: the manifest schema has no field that could
//     carry custom_env, custom_args, mcp_config, runtime IDs, or webhook
//     tokens. Sanitisation is structural, not a strip pass.
//   - Cross-references are by role key, never UUID — manifests must be
//     meaningful outside their source workspace.
//   - Parameters are declared up front; instructions and skill content
//     reference them as {{param.<key>}} placeholders. Rendering is plain
//     substitution at apply time; smart rewriting belongs to the intake
//     adaptation pass, not the template engine.
package teamtmpl

import (
	"fmt"
	"regexp"
	"strings"
)

// FormatVersion is bumped when the manifest schema changes shape. Readers
// must reject manifests with a format they don't understand.
const FormatVersion = 1

// Manifest is the whole template document stored in
// team_template_version.manifest.
type Manifest struct {
	Format      int         `json:"format"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  []Parameter `json:"parameters,omitempty"`
	Roles       []Role      `json:"roles"`
	Skills      []Skill     `json:"skills,omitempty"`
	Squad       *Squad      `json:"squad,omitempty"`
	Workflows   []Workflow  `json:"workflows,omitempty"`
	Autopilots  []Autopilot `json:"autopilots,omitempty"`
	Provenance  Provenance  `json:"provenance"`
}

// Parameter declares a project-specific value the applier must supply
// (repo URL, default stack, human owner, escalation contact, ...).
type Parameter struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Example     string `json:"example,omitempty"`
}

// Role is one agent of the team, identified by a slug key that squad
// members, workflow steps, and autopilot assignees reference.
type Role struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions"`
	// Provider is the runtime family this role needs (e.g. "claude",
	// "codex", "gemini"). Apply maps providers to concrete runtime IDs in
	// the target workspace.
	Provider           string `json:"provider,omitempty"`
	Model              string `json:"model,omitempty"`
	ThinkingLevel      string `json:"thinking_level,omitempty"`
	MaxConcurrentTasks int32  `json:"max_concurrent_tasks,omitempty"`
	AvatarURL          string `json:"avatar_url,omitempty"`
	// Skills lists manifest skill names bound to this role.
	Skills []string `json:"skills,omitempty"`
}

// Skill embeds a full transferable skill (the approved cross-project pack
// only — inclusion is an explicit export-time selection).
type Skill struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Content     string      `json:"content"`
	Files       []SkillFile `json:"files,omitempty"`
	// Source records where this copy came from, for the ADA-32 governed
	// promotion path to reconcile against later.
	Source SkillSource `json:"source"`
}

type SkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type SkillSource struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
	SkillID     string `json:"skill_id,omitempty"`
	ExportedAt  string `json:"exported_at,omitempty"`
}

// Squad captures the team shape. Only agent members are exported — the
// humans on a client engagement are never the source workspace's humans.
type Squad struct {
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	Instructions string        `json:"instructions,omitempty"`
	LeaderRole   string        `json:"leader_role"`
	Members      []SquadMember `json:"members"`
}

type SquadMember struct {
	Role      string `json:"role"`
	SquadRole string `json:"squad_role,omitempty"`
}

// Workflow mirrors the handoff-workflow definition with agent references
// replaced by role keys.
type Workflow struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Steps       []WorkflowStep `json:"steps"`
}

type WorkflowStep struct {
	Order         int32  `json:"order"`
	Name          string `json:"name,omitempty"`
	Role          string `json:"role"`
	StartStatus   string `json:"start_status"`
	AdvanceStatus string `json:"advance_status"`
}

// Autopilot mirrors the autopilot definition. Webhook tokens and signing
// secrets are never exported; webhook triggers carry kind+label only and
// get fresh credentials at apply time.
type Autopilot struct {
	Title              string             `json:"title"`
	Description        string             `json:"description,omitempty"`
	AssigneeRole       string             `json:"assignee_role,omitempty"`
	AssigneeSquad      bool               `json:"assignee_squad,omitempty"`
	ExecutionMode      string             `json:"execution_mode"`
	IssueTitleTemplate string             `json:"issue_title_template,omitempty"`
	ConcurrencyPolicy  string             `json:"concurrency_policy,omitempty"`
	Triggers           []AutopilotTrigger `json:"triggers,omitempty"`
}

type AutopilotTrigger struct {
	Kind           string `json:"kind"`
	CronExpression string `json:"cron_expression,omitempty"`
	Timezone       string `json:"timezone,omitempty"`
	Label          string `json:"label,omitempty"`
}

// Provenance records where the manifest came from (acceptance criterion
// ADA-30/10). IDs are recorded for audit, never resolved at apply time.
type Provenance struct {
	SourceWorkspaceID string `json:"source_workspace_id"`
	ExportedBy        string `json:"exported_by,omitempty"`
	ExportedAt        string `json:"exported_at"`
}

var (
	roleKeyRe  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	paramKeyRe = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)
	// placeholderRe matches {{param.<key>}} references inside text.
	placeholderRe = regexp.MustCompile(`\{\{\s*param\.([a-zA-Z0-9_]+)\s*\}\}`)
)

// SlugifyRoleKey derives a role key from an agent name ("Tech Lead
// (Codex)" -> "tech-lead-codex").
func SlugifyRoleKey(name string) string {
	var b strings.Builder
	lastDash := true // suppress leading dash
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// Validate checks referential and structural integrity. It does NOT lint
// content — see Lint for the autonomy/secret gates.
func (m *Manifest) Validate() error {
	if m.Format != FormatVersion {
		return fmt.Errorf("unsupported manifest format %d (expected %d)", m.Format, FormatVersion)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("manifest name is required")
	}
	if len(m.Roles) == 0 {
		return fmt.Errorf("manifest must contain at least one role")
	}

	params := make(map[string]bool, len(m.Parameters))
	for _, p := range m.Parameters {
		if !paramKeyRe.MatchString(p.Key) {
			return fmt.Errorf("parameter key %q is not snake_case", p.Key)
		}
		if params[p.Key] {
			return fmt.Errorf("duplicate parameter key %q", p.Key)
		}
		params[p.Key] = true
	}

	skills := make(map[string]bool, len(m.Skills))
	for _, s := range m.Skills {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("skill with empty name")
		}
		if skills[s.Name] {
			return fmt.Errorf("duplicate skill %q", s.Name)
		}
		skills[s.Name] = true
	}

	roles := make(map[string]bool, len(m.Roles))
	for _, r := range m.Roles {
		if !roleKeyRe.MatchString(r.Key) {
			return fmt.Errorf("role key %q is not kebab-case", r.Key)
		}
		if roles[r.Key] {
			return fmt.Errorf("duplicate role key %q", r.Key)
		}
		roles[r.Key] = true
		if strings.TrimSpace(r.Instructions) == "" {
			return fmt.Errorf("role %q has empty instructions", r.Key)
		}
		for _, sk := range r.Skills {
			if !skills[sk] {
				return fmt.Errorf("role %q references skill %q which is not in the manifest", r.Key, sk)
			}
		}
	}

	if m.Squad != nil {
		if !roles[m.Squad.LeaderRole] {
			return fmt.Errorf("squad leader role %q is not in the manifest", m.Squad.LeaderRole)
		}
		seen := make(map[string]bool, len(m.Squad.Members))
		for _, mem := range m.Squad.Members {
			if !roles[mem.Role] {
				return fmt.Errorf("squad member role %q is not in the manifest", mem.Role)
			}
			if seen[mem.Role] {
				return fmt.Errorf("duplicate squad member role %q", mem.Role)
			}
			seen[mem.Role] = true
		}
	}

	for _, wf := range m.Workflows {
		if strings.TrimSpace(wf.Name) == "" {
			return fmt.Errorf("workflow with empty name")
		}
		if len(wf.Steps) == 0 {
			return fmt.Errorf("workflow %q has no steps", wf.Name)
		}
		for _, st := range wf.Steps {
			if !roles[st.Role] {
				return fmt.Errorf("workflow %q step %d references role %q which is not in the manifest", wf.Name, st.Order, st.Role)
			}
		}
	}

	for _, ap := range m.Autopilots {
		if strings.TrimSpace(ap.Title) == "" {
			return fmt.Errorf("autopilot with empty title")
		}
		if ap.AssigneeSquad {
			if m.Squad == nil {
				return fmt.Errorf("autopilot %q is squad-assigned but the manifest has no squad", ap.Title)
			}
		} else if ap.AssigneeRole != "" && !roles[ap.AssigneeRole] {
			return fmt.Errorf("autopilot %q references role %q which is not in the manifest", ap.Title, ap.AssigneeRole)
		}
	}

	// Every {{param.x}} placeholder anywhere in the manifest must be
	// declared — an undeclared placeholder would silently survive
	// rendering and ship literal mustache braces into a client project.
	for _, loc := range m.textLocations() {
		for _, match := range placeholderRe.FindAllStringSubmatch(loc.text, -1) {
			if !params[match[1]] {
				return fmt.Errorf("%s references undeclared parameter %q", loc.where, match[1])
			}
		}
	}

	return nil
}

// textLocation pairs a human-addressable location with the text it holds,
// for placeholder validation and content linting.
type textLocation struct {
	where string
	text  string
}

func (m *Manifest) textLocations() []textLocation {
	locs := []textLocation{
		{"manifest description", m.Description},
	}
	for _, r := range m.Roles {
		locs = append(locs,
			textLocation{fmt.Sprintf("role %q description", r.Key), r.Description},
			textLocation{fmt.Sprintf("role %q instructions", r.Key), r.Instructions},
		)
	}
	for _, s := range m.Skills {
		locs = append(locs,
			textLocation{fmt.Sprintf("skill %q description", s.Name), s.Description},
			textLocation{fmt.Sprintf("skill %q content", s.Name), s.Content},
		)
		for _, f := range s.Files {
			locs = append(locs, textLocation{fmt.Sprintf("skill %q file %q", s.Name, f.Path), f.Content})
		}
	}
	if m.Squad != nil {
		locs = append(locs,
			textLocation{"squad description", m.Squad.Description},
			textLocation{"squad instructions", m.Squad.Instructions},
		)
	}
	for _, wf := range m.Workflows {
		locs = append(locs, textLocation{fmt.Sprintf("workflow %q description", wf.Name), wf.Description})
		for _, st := range wf.Steps {
			locs = append(locs, textLocation{fmt.Sprintf("workflow %q step %d name", wf.Name, st.Order), st.Name})
		}
	}
	for _, ap := range m.Autopilots {
		locs = append(locs,
			textLocation{fmt.Sprintf("autopilot %q description", ap.Title), ap.Description},
			textLocation{fmt.Sprintf("autopilot %q issue title template", ap.Title), ap.IssueTitleTemplate},
		)
	}
	return locs
}
