package teamtmpl

import (
	"encoding/json"
	"strings"
	"testing"
)

func validManifest() *Manifest {
	return &Manifest{
		Format:      FormatVersion,
		Name:        "Delivery Team",
		Description: "Standard Yozu delivery team",
		Parameters: []Parameter{
			{Key: "repo_url", Label: "Repository URL", Required: true},
			{Key: "human_owner", Label: "Human owner", Required: true},
		},
		Roles: []Role{
			{
				Key:          "tech-lead",
				Name:         "Tech Lead",
				Instructions: "You own technical direction for {{param.repo_url}}. Escalate to {{param.human_owner}}.",
				Provider:     "claude",
				Model:        "claude-fable-5",
				Skills:       []string{"api-design"},
			},
			{
				Key:          "backend-engineer",
				Name:         "Backend Engineer",
				Instructions: "Implement well-specified Go changes.",
				Provider:     "codex",
			},
		},
		Skills: []Skill{
			{Name: "api-design", Content: "# API design\nDesign endpoints for the fork."},
		},
		Squad: &Squad{
			Name:       "Delivery Squad",
			LeaderRole: "tech-lead",
			Members: []SquadMember{
				{Role: "tech-lead", SquadRole: "leader"},
				{Role: "backend-engineer"},
			},
		},
		Workflows: []Workflow{
			{
				Name: "Build & review",
				Steps: []WorkflowStep{
					{Order: 1, Role: "backend-engineer", StartStatus: "todo", AdvanceStatus: "in_review"},
					{Order: 2, Role: "tech-lead", StartStatus: "todo", AdvanceStatus: "in_review"},
				},
			},
		},
		Autopilots: []Autopilot{
			{
				Title:         "Wave manager",
				AssigneeRole:  "tech-lead",
				ExecutionMode: "run_only",
				Triggers:      []AutopilotTrigger{{Kind: "schedule", CronExpression: "0 * * * *", Timezone: "Europe/London"}},
			},
		},
		Provenance: Provenance{SourceWorkspaceID: "ws-1", ExportedAt: "2026-06-12T00:00:00Z"},
	}
}

func TestValidateAcceptsWellFormedManifest(t *testing.T) {
	if err := validManifest().Validate(); err != nil {
		t.Fatalf("expected valid manifest, got: %v", err)
	}
}

func TestValidateRejections(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Manifest)
		wantErr string
	}{
		{"wrong format", func(m *Manifest) { m.Format = 99 }, "unsupported manifest format"},
		{"empty name", func(m *Manifest) { m.Name = " " }, "name is required"},
		{"no roles", func(m *Manifest) { m.Roles = nil }, "at least one role"},
		{"bad role key", func(m *Manifest) { m.Roles[0].Key = "Tech Lead" }, "not kebab-case"},
		{"duplicate role key", func(m *Manifest) { m.Roles[1].Key = "tech-lead" }, "duplicate role key"},
		{"empty instructions", func(m *Manifest) { m.Roles[0].Instructions = "" }, "empty instructions"},
		{"unknown role skill", func(m *Manifest) { m.Roles[0].Skills = []string{"missing"} }, "references skill"},
		{"bad param key", func(m *Manifest) { m.Parameters[0].Key = "repo-url" }, "not snake_case"},
		{"duplicate param", func(m *Manifest) { m.Parameters[1].Key = "repo_url" }, "duplicate parameter"},
		{"duplicate skill", func(m *Manifest) {
			m.Skills = append(m.Skills, Skill{Name: "api-design", Content: "x"})
		}, "duplicate skill"},
		{"unknown squad leader", func(m *Manifest) { m.Squad.LeaderRole = "ghost" }, "squad leader role"},
		{"unknown squad member", func(m *Manifest) { m.Squad.Members[1].Role = "ghost" }, "squad member role"},
		{"workflow without steps", func(m *Manifest) { m.Workflows[0].Steps = nil }, "has no steps"},
		{"workflow unknown role", func(m *Manifest) { m.Workflows[0].Steps[0].Role = "ghost" }, "references role"},
		{"autopilot unknown role", func(m *Manifest) { m.Autopilots[0].AssigneeRole = "ghost" }, "references role"},
		{"autopilot squad without squad", func(m *Manifest) {
			m.Autopilots[0].AssigneeRole = ""
			m.Autopilots[0].AssigneeSquad = true
			m.Squad = nil
		}, "no squad"},
		{"undeclared placeholder", func(m *Manifest) {
			m.Roles[0].Instructions = "Deploy to {{param.deploy_url}}."
		}, "undeclared parameter"},
		{"undeclared placeholder in skill file", func(m *Manifest) {
			m.Skills[0].Files = []SkillFile{{Path: "ref.md", Content: "see {{param.ghost_key}}"}}
		}, "undeclared parameter"},
		{"undeclared placeholder in workflow description", func(m *Manifest) {
			m.Workflows[0].Description = "Ships to {{param.ghost_key}}."
		}, "undeclared parameter"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest()
			tc.mutate(m)
			err := m.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestManifestSchemaHasNoSecretBearingFields(t *testing.T) {
	// Structural sanitisation guarantee: serialising a manifest must never
	// produce the field names that carry secrets or instance-local wiring
	// on the live entities. If someone adds such a field to the schema,
	// this test is the tripwire.
	raw, err := json.Marshal(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"custom_env", "custom_args", "mcp_config", "runtime_id", "webhook_token", "signing_secret"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("manifest JSON contains banned field %q", banned)
		}
	}
}

func TestLintAutonomyLanguage(t *testing.T) {
	m := validManifest()
	m.Roles[0].Instructions = "The team has standing authorisation to deliver without per-issue human sign-off (ADA-1)."
	findings := Lint(m)
	if len(findings) < 2 {
		t.Fatalf("expected multiple autonomy findings, got %d: %+v", len(findings), findings)
	}
	for _, f := range findings {
		if f.Rule != "autonomy_language" {
			t.Errorf("unexpected rule %q", f.Rule)
		}
		if f.Location != `role "tech-lead" instructions` {
			t.Errorf("unexpected location %q", f.Location)
		}
	}
}

func TestLintBlockedSkill(t *testing.T) {
	m := validManifest()
	m.Skills = append(m.Skills, Skill{Name: "ada-house-rules", Content: "house rules"})
	m.Roles[0].Skills = append(m.Roles[0].Skills, "ada-house-rules")
	findings := Lint(m)
	found := false
	for _, f := range findings {
		if f.Rule == "blocked_skill" && f.Marker == "ada-house-rules" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected blocked_skill finding, got: %+v", findings)
	}
}

func TestLintSecretPatternsAreRedacted(t *testing.T) {
	m := validManifest()
	m.Skills[0].Files = []SkillFile{{
		Path:    "setup.md",
		Content: "export GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789",
	}}
	findings := Lint(m)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.Rule != "secret_pattern" || f.Marker != "github_token" {
		t.Fatalf("unexpected finding: %+v", f)
	}
	if strings.Contains(f.Excerpt, "ghp_") {
		t.Fatalf("secret leaked into lint excerpt: %q", f.Excerpt)
	}
}

func TestLintCoversWorkflowText(t *testing.T) {
	m := validManifest()
	m.Workflows[0].Description = "Runs with standing authorisation."
	m.Workflows[0].Steps[0].Name = "Build per ADA-1"
	findings := Lint(m)
	locations := make(map[string]bool, len(findings))
	for _, f := range findings {
		if f.Rule != "autonomy_language" {
			t.Errorf("unexpected rule %q", f.Rule)
		}
		locations[f.Location] = true
	}
	if !locations[`workflow "Build & review" description`] {
		t.Errorf("no finding for workflow description, got: %+v", findings)
	}
	if !locations[`workflow "Build & review" step 1 name`] {
		t.Errorf("no finding for workflow step name, got: %+v", findings)
	}
}

func TestLintCleanManifestHasNoFindings(t *testing.T) {
	if findings := Lint(validManifest()); len(findings) != 0 {
		t.Fatalf("expected no findings, got: %+v", findings)
	}
}

func TestSubstituteRewritesAllTextLocations(t *testing.T) {
	m := validManifest()
	m.Roles[0].Instructions = "Clone https://github.com/acme/widgets and read the docs."
	m.Skills[0].Content = "Run checks against https://github.com/acme/widgets."
	m.Skills[0].Files = []SkillFile{{Path: "ref.md", Content: "repo: https://github.com/acme/widgets"}}
	m.Parameters = append(m.Parameters, Parameter{Key: "the_repo", Label: "Repo", Required: true})

	n := Substitute(m, Substitution{Find: "https://github.com/acme/widgets", ParamKey: "the_repo"})
	if n != 3 {
		t.Fatalf("expected 3 replacements, got %d", n)
	}
	if !strings.Contains(m.Roles[0].Instructions, "{{param.the_repo}}") {
		t.Errorf("instructions not substituted: %q", m.Roles[0].Instructions)
	}
	if strings.Contains(m.Skills[0].Files[0].Content, "acme/widgets") {
		t.Errorf("skill file not substituted: %q", m.Skills[0].Files[0].Content)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest invalid after substitution: %v", err)
	}
}

func TestSubstituteCoversWorkflowText(t *testing.T) {
	m := validManifest()
	m.Workflows[0].Description = "Delivery loop for https://github.com/acme/widgets."
	m.Workflows[0].Steps[0].Name = "Build https://github.com/acme/widgets"
	m.Parameters = append(m.Parameters, Parameter{Key: "the_repo", Label: "Repo", Required: true})

	n := Substitute(m, Substitution{Find: "https://github.com/acme/widgets", ParamKey: "the_repo"})
	if n != 2 {
		t.Fatalf("expected 2 replacements, got %d", n)
	}
	if !strings.Contains(m.Workflows[0].Description, "{{param.the_repo}}") {
		t.Errorf("workflow description not substituted: %q", m.Workflows[0].Description)
	}
	if !strings.Contains(m.Workflows[0].Steps[0].Name, "{{param.the_repo}}") {
		t.Errorf("workflow step name not substituted: %q", m.Workflows[0].Steps[0].Name)
	}
}

func TestTextLocationsAndPointersStayInSync(t *testing.T) {
	// textLocations (read path: validation + lint) and textPointers (write
	// path: substitution) must cover the same fields in the same order. A
	// schema field added to one but not the other escapes a gate — exactly
	// the workflow-text bug this guards against recurring.
	m := validManifest()
	m.Workflows[0].Description = "wf desc"
	locs := m.textLocations()
	ptrs := m.textPointers()
	if len(locs) != len(ptrs) {
		t.Fatalf("textLocations has %d entries, textPointers has %d — the two walks diverged", len(locs), len(ptrs))
	}
	for i := range locs {
		if locs[i].text != *ptrs[i] {
			t.Errorf("entry %d (%s): location text %q != pointer text %q", i, locs[i].where, locs[i].text, *ptrs[i])
		}
	}
}

func TestSubstituteEmptyFindIsNoop(t *testing.T) {
	m := validManifest()
	if n := Substitute(m, Substitution{Find: "", ParamKey: "repo_url"}); n != 0 {
		t.Fatalf("expected 0 replacements, got %d", n)
	}
}

func TestSlugifyRoleKey(t *testing.T) {
	cases := map[string]string{
		"Tech Lead":         "tech-lead",
		"Tech Lead (Codex)": "tech-lead-codex",
		"  QA  Engineer  ":  "qa-engineer",
		"Fable Engineer":    "fable-engineer",
	}
	for in, want := range cases {
		if got := SlugifyRoleKey(in); got != want {
			t.Errorf("SlugifyRoleKey(%q) = %q, want %q", in, got, want)
		}
	}
}
