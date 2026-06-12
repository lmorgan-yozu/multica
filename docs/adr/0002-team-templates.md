# 0002 — Team templates are DB-backed versioned manifests exported from a live workspace

**Status**: accepted (ADA-30, 2026-06-12)

## Context

Standing up a new engagement on Ada means recreating the delivery team —
role agents, their instructions, skill bindings, squad shape, workflow
chains, autopilots — in a fresh workspace, parameterised for the client
project. Today that knowledge lives only in the Ada Bootstrapping
workspace's live rows. The existing `agenttmpl` registry is a static,
in-repo, single-agent picker; it cannot capture a whole team, cannot be
exported from a running workspace, and changes require a deploy. Exported
material must never carry secrets (`custom_env`), and must never carry the
ADA-1 autonomous-mode exception into a client project.

## Decision

We will store team templates as rows: `team_template` (workspace-owned
identity) plus `team_template_version` (immutable, append-only JSONB
manifest per export). A manifest is a self-contained, format-versioned
document: parameter declarations, roles (instructions, model, thinking
level, provider requirement, skill bindings), embedded skills (content +
files + provenance), squad shape, workflow definitions, and autopilot
definitions — all cross-references by role key, never by UUID.

Export (`POST /api/team-templates/export`) snapshots explicitly selected
entities from the caller's workspace. The manifest builder takes a
whitelist of fields, so `custom_env`, `custom_args`, `mcp_config`, runtime
IDs, and webhook tokens are excluded by construction. Export fails closed
(422 with structured findings) when an autonomy-language lint or a
secret-pattern lint hits, or when a selected workflow/autopilot/squad
references an agent outside the selection.

Apply (separate slice) renders parameters into a target workspace/project
and is always human-initiated; applied autopilots start paused and applied
projects start in intake state.

## Consequences

- Easier: templates evolve without deploys; every export is a recorded,
  diffable version with provenance (source workspace, exporter, time);
  round-trip improvements from client projects land as new versions.
- Easier: sanitisation is testable at one choke point (builder + lint).
- Harder: embedded skills are copies, not live references — drift between
  a client workspace's skill and the template is reconciled only through
  the governed promotion path (ADA-32), by exporting a new version.
- Accepted: manifest JSONB is schemaless at the DB layer; integrity is
  enforced by the Go validator at write time, and `format` gives us a
  migration handle.

## Rejected

- **Extend the in-repo `agenttmpl` registry** — static JSON in source
  requires a deploy per template change and cannot snapshot a live
  workspace; it stays as the single-agent picker it is.
- **Live cross-workspace references (template points at source rows)** —
  breaks tenant isolation, makes templates mutate under consumers, and
  leaks source-workspace changes into client projects without review.
- **Normalised template tables mirroring every entity** — heavy schema
  surface for a document that is written once and read whole; JSONB +
  validator gives the same integrity with one table.
- **Export-everything with a strip list (blocklist)** — one missed field
  leaks a secret; whitelist construction fails safe.
