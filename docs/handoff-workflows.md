# Handoff Workflows (role-based agent orchestration)

Status: **proposed / first slice implemented** — backend engine + data model + CLI.
Owner: needs human architecture sign-off before production rollout.

## Why

The Ada vision calls for automated agent-to-agent handoffs: "a backend agent
completes the API endpoint, and the workflow then reassigns the ticket to the
frontend agent for the UI, then to QA for test coverage, with the appropriate
context passed along at each transition. At given points a human engineer
reviews." Today Multica only routes work manually — via `@mention` in a comment
or a human reassigning the issue. There is no ordered, role-based handoff chain.

This feature adds **workflows**: a named, ordered list of steps, each step bound
to an agent. An issue can be placed on a workflow; when the agent on the current
step finishes (its issue reaches the step's `advance_status`, e.g. `in_review`),
the platform automatically advances to the next step — reassigning the issue,
posting a structured handoff comment, and triggering the next agent. The final
step leaves the issue at `advance_status` as a **human review gate** rather than
auto-closing.

## Design principles

- **Strictly opt-in.** Nothing changes for any issue unless it has a row in
  `issue_workflow_run`. Issues not on a workflow behave exactly as before. This
  bounds blast radius — critical on a live platform.
- **Mirror the proven `notifyParentOfChildDone` path.** The advance engine is a
  best-effort, post-commit hook in the issue update handler, alongside the
  existing child-done notification. Same conventions: system comments, explicit
  `EnqueueTaskForMention`, `HasPendingTaskForIssueAndAgent` idempotency, errors
  logged and swallowed (never roll back the user's status change).
- **Transition-triggered + idempotent.** Advance fires only on the exact
  transition *into* `advance_status`, only while the run is `active`, and only
  while the issue is still assigned to the current step's agent. Re-saving the
  same status, or a human reassigning mid-flight, does not advance.

## Data model (migration `116_handoff_workflows`)

```
workflow(id, workspace_id, name, description,
         created_by_type, created_by_id, created_at, updated_at, archived_at)

workflow_step(id, workflow_id, step_order, agent_id, name,
              start_status   DEFAULT 'todo',       -- status set when this step begins (drives the agent run)
              advance_status DEFAULT 'in_review',  -- status that signals this step is complete
              created_at, UNIQUE(workflow_id, step_order))

issue_workflow_run(id, issue_id UNIQUE, workflow_id, current_step_id,
                   state DEFAULT 'active',  -- active | completed | cancelled
                   created_at, updated_at)
```

## Advance engine

`advanceWorkflowOnStatusChange(ctx, prev, issue, actorType, actorID)`:

1. Load the `issue_workflow_run` for the issue. None → return (opt-in no-op).
2. Run must be `active`; load current step.
3. `stepShouldAdvance(...)` guard — transition into `advance_status`, issue still
   assigned to the step's agent. Pure function, unit-tested.
4. Find the next step (smallest `step_order` greater than current):
   - **No next step (final):** mark run `completed`, post a "ready for human
     review" system comment, leave the issue as-is (already at `advance_status`).
   - **Next step exists:** point the run at it, reassign the issue to the next
     agent + set status to the next step's `start_status`, write a structured
     handoff record (see below), post a handoff system comment mentioning the
     next agent, and enqueue the next agent's task (idempotency-guarded).

Hooked in `UpdateIssue` and `BatchUpdateIssues` right after
`notifyParentOfChildDone`, gated on `statusChanged`.

## Structured handoff records (migration `118_issue_handoff`, ADA-18/ADA-23)

Advancing a step writes an `issue_handoff` row — the source of truth for the
transition; the system comment is only the timeline notification:

- If the issue's latest handoff was authored by the outgoing step's agent and
  is not yet linked to a workflow run (the agent recorded its own handoff
  before flipping the status), the advance **adopts** it: stamps
  `workflow_run_id`/`workflow_step_id` (the completed step) and the routing
  decision (`next_assignee_*` = the next step's agent) onto that record
  rather than inserting a thin duplicate that would shadow it as "latest".
- Otherwise it **inserts** a synthesized record: author = outgoing step's
  agent, next assignee = next step's agent, run/step linkage, and the
  outgoing agent's most recent task on the issue where available.
- Run completion (final step) is a human review gate, not a handoff — no
  record is written there.

On the read side, the daemon claim endpoint (`ClaimTaskByRuntime`) attaches
the issue's latest handoff (with display names resolved) to the claim
response as `latest_handoff`, and `daemon.BuildPrompt` renders it as a
`## Latest handoff` block: as starting context for direct
(assignment/workflow-triggered) tasks, and as explicitly-background issue
state for comment-triggered tasks (the triggering comment stays the task).
Issues with no handoffs claim and prompt exactly as before.

## CLI

```
multica workflow create  --name <n> [--description <d>]
multica workflow add-step <workflow-id> --agent <agent-id> [--name <n>] \
                          [--start-status todo] [--advance-status in_review]
multica workflow bind     <workflow-id> --issue <issue-id>   # starts run at step 1
multica workflow show     <workflow-id>
multica workflow list
```

## Not in this slice (follow-ups)

- Frontend UI for defining/visualising workflows and an issue's position in one.
- Conditional routing / branching (e.g. QA failure routes back to dev).
- Per-step model overrides and per-step skills.
- Loop/spend braking integrated with the handoff chain.

## Rollout

Build + test on a branch → deploy to the **staging** stack
(`multica-*-staging`) → demo → human architecture sign-off → production. The
migration is additive (new tables only); the down migration drops them cleanly.
