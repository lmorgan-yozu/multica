# Gap analysis: this fork vs the Ada vision

Audience: the Ada platform team (agents and humans). State as of June 2026,
fork at `main` = 2520e119. Each gap maps to a backlog epic in the Ada
workspace ("Ada Bootstrapping" project).

## Already in the fork (beyond upstream Multica)

| Capability | Where |
| --- | --- |
| Role-based handoff workflows: ordered steps, per-step agent, auto-advance on status transition, structured handoff comment, human gate at the end | migration `116_handoff_workflows`, advance engine in the issue update path, web UI (`feat(workflows)` commits); `docs/handoff-workflows.md` |
| Document library: issue/project/workspace documents, versioning, comments, curation, bulk export, summarise | `feat(docs)` commits; document-viewer feature |
| Per-agent model + per-role agents | upstream Multica: agent records carry `model`, `instructions`, skills bindings |
| Skills library with supporting files, agent binding | upstream Multica skills + workspace skills |
| Autopilots (scheduled/triggered agent automations) | upstream; proven in the Arcus FM workspace (wave-manager pattern) |
| Squads with leader routing | upstream |
| Run observability (task records, comments, status history) | upstream |

## Gaps (epic-by-epic)

| Epic | Gap | Builds on |
| --- | --- | --- |
| ADA-2 Structured handoff records | Handoff content is free-text comments; no first-class record (done / remaining / decisions / uncertainties) stored per session, surfaced in the timeline, and injected into the next agent's task brief | workflows feature, task lifecycle |
| ADA-3 Spend visibility & loop detection | No per-issue/per-agent/per-day token aggregation; no climbing-spend-without-progress detection; no automatic brake | task usage records |
| ADA-4 Enforced quality gates | Implementer ≠ approver is convention (agent instructions) only; nothing stops the implementing agent advancing its own issue; no required-review chain enforcement | status-transition API, workflows |
| ADA-5 Delivery metrics | No metrics surface: review rejection rate, comments per issue, defect escape, cycle time, throughput | issue/status/task history in Postgres |
| ADA-6 Agent memory | No shared long-term memory; pgvector substrate already present | handoffs (ADA-2) as a memory source |
| ADA-7 PM workflows | No RAID log, budget burn, or status-report workflows | autopilots, documents library, spend (ADA-3) |
| ADA-8 QA workflows | No evidence packs or release-readiness reporting | documents library, skills |

## Sequencing rationale

ADA-2/3/4 first: they are the platform's safety and context spine, and
ADA-5/6/7 each consume their outputs. ADA-2 before ADA-6 (handoffs feed
memory). ADA-3 before ADA-7's budget burn. ADA-8 is mostly skills plus small
affordances and can trail.

## Standing constraints

- Additive, well-isolated changes over invasive rewrites — this fork tracks
  a fast-moving upstream, and everything ships behind opt-in where feasible
  (the workflows feature's "strictly opt-in, bounded blast radius" pattern
  is the template).
- The live instance runs this code; migrations must be reversible and
  staging-tested. While the DEFAULT model demands human-gated prod deploys
  (and this applies to all ADA-30 template exports), this specific workspace
  operates under the ADA-1 exception: standing build authorisation and strict
  backup+rollback discipline for risky moves.
- Human override must always exist for any enforcement feature (audited
  owner/admin force-transition).
