# 0001. Roadmap data model: derived epics, milestone table, revived issue_dependency

- **Status**: accepted
- **Date**: 2026-06-11
- **Issue**: ADA-57 (parent ADA-54, Project Roadmap)

## Context

The Ada roadmap must be derived from the project/issue graph, not
hand-maintained — anything an agent or human edits by hand drifts within a
day on a board this active. The issue tree already carries most of the
signal (parents, statuses, dates, position), but two things have no durable
home: grouping epics under dated checkpoints, and explicit "X must land
before Y" links where dates and positions are not enough. We work in a fork
of upstream Multica, so every new table or column widens the surface we
must carry across syncs (see the working-with-upstream rules).

## Decision

Three parts, all additive (migration `118_project_roadmap`, originally
numbered 117 and renumbered after a concurrent migration landed on main):

1. **Epics stay derived.** A project's top-level issues are its epic nodes.
   No epic flag, no epic table. Leaf issue statuses roll up to epic
   progress (`done`/`total`); childless top-level issues appear as
   single-issue epics counting themselves, rather than being filtered out
   silently.
2. **Dependencies revive the dormant `issue_dependency` table** that has
   existed since `001_init` with no write path. Write convention: one row
   per link with `type = 'blocked_by'`, read as "`issue_id` depends on
   `depends_on_issue_id`". Migration 118 hardens it in place: `created_at`,
   a self-link CHECK, a unique `(issue_id, depends_on_issue_id, type)`
   index, and lookup indexes. Writes validate same-project membership and
   reject cycles; the read path degrades with a `cycle_detected` flag if
   bad data pre-exists.
3. **Milestones get a new `milestone` table** — `project_id` FK (CASCADE),
   `name`, `description`, `target_date DATE`, `position` — plus a nullable
   `issue.milestone_id` FK (`ON DELETE SET NULL`). One milestone per issue.
   `target_date` is `DATE`, not `TIMESTAMPTZ`: a target is a calendar day,
   and migration 112 already taught us timestamptz shifts displayed days
   across timezones. The table is named `milestone` rather than the
   `project_milestone` the ADA-57 ruling sketched: the `project_id NOT
   NULL` FK already states the ownership, matching how `issue`, `label`,
   and `project_resource` are named in this schema.

## Consequences

- Easier: the roadmap projection (`GET /api/projects/{id}/roadmap`) is a
  pure function over existing issue rows plus two small tables; an empty
  project or a project with zero roadmap metadata still yields a useful
  ordered roadmap.
- Easier: upstream syncs — `issue_dependency` keeps its upstream shape
  (only gains columns/indexes), and `milestone` is a fork-local table that
  cannot conflict.
- Harder: we own cycle prevention in application code (insert-time graph
  walk) because SQL cannot express it; the graphs are project-sized, so
  walking them is cheap.
- Accepted: reviving `issue_dependency` assumes the table is empty in
  every live environment (it has never had a write path). Verify with a
  `SELECT count(*)` before running migration 118 in staging/prod.
- Accepted: one-milestone-per-issue means re-planning moves an epic, never
  shares it. If a real many-to-many need appears, that is a new ADR.

## Rejected

- **New `epic_dependency` (or similar) table** — duplicates a dormant
  upstream table and widens fork divergence for nothing.
- **Milestone as a label or issue-metadata key** — no typed `target_date`,
  no referential integrity, no `ON DELETE` semantics; grouping becomes
  string matching.
- **Milestone as a sub-project** — heavyweight; projects are containers
  with members and resources, not orderable checkpoints on a timeline.
- **Many-to-many epic↔milestone join table** — YAGNI; it muddies progress
  rollup (double-counted leaves) and nothing in the Ada vision needs an
  epic in two milestones.
- **Persisted epic flag/table** — a second source of truth for something
  the issue tree already encodes; would drift the moment an issue gains or
  loses children.
