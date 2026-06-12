# The Ada platform team

Audience: agents and humans in the Ada workspace ("Ada Bootstrapping"
project). This documents the agent team that builds Ada, set up June 2026
from the patterns proven in the Arcus FM workspace.

## Shared ground rules

Every agent is bound to the `ada-house-rules` workspace skill: plain
British English, small reviewable changes, feature-branch flow on the
private fork, structured hand-off comments, status + reassignment on every
hand-off, and a hard escalation list (scope/cost, prod deploys, fundamental
architecture, ambiguity, anything touching the public Multica project).
Engineers and QA are additionally bound to the `tdd` skill.

## Roster

| Role | Model / runtime | Purpose |
| --- | --- | --- |
| Product Owner | pro (Gemini) | Direction/PM-shaped thinking; owns the backlog and acceptance against the vision docs; casting vote on product intent |
| UX/UI Designer | pro (Gemini) | Direction/PM-shaped thinking; design direction and user experience |
| Delivery Lead | gpt-5.5 (Codex) | Flow, risk, and status; squad leader (conductor); watches for stalls and token burn; cap-proof reliability |
| Tech Lead | claude-fable-5 (Claude) | Technical direction and breakdown; casting vote on technical deadlocks; second-line on Fable |
| Tech Lead (Codex) | gpt-5.5 (Codex) | Understudy for cap overflow |
| Backend Engineer | gpt-5.5 (Codex) | Go server, schema, API, daemon/CLI |
| Frontend Engineer | gpt-5.5 (Codex) | Next.js web app |
| Fable Engineer (HIGH COST) | claude-fable-5 (Claude) | Complex, ambiguous, cross-cutting work only — Tech Lead must justify its use |
| Code Reviewer | claude-4.6-sonnet (Claude) | Frequent-deep reviews; primary quality gate (preserves Fable cap for the deepest work) |
| Code Reviewer (Codex) | gpt-5.5 (Codex) | Understudy for cap overflow |
| QA Engineer | gpt-5.5 (Codex) | Verification against acceptance criteria; release-readiness casting vote |
| Security Reviewer | claude-fable-5 (Claude) | Auth, tenant isolation, secrets, untrusted input; verdict is a veto |
| DevOps Engineer | gpt-5.5 (Codex) | Builds and deploys to staging; prod is always human-gated |
| Technical Writer | pro (Gemini) | Keeps fork docs and runbooks accurate |

Squad: **Ada Platform Team**, led by the Delivery Lead. Squad-routed work
goes to the leader, who conducts but never implements.

## Review chain (implementer ≠ approver, by construction)

```
Engineer (in_progress → in_review, reassign Code Reviewer)
  → Code Reviewer (approve → reassign QA; or back to engineer)
    → QA Engineer (pass → reassign Product Owner; or back to engineer)
      → Product Owner acceptance (→ DevOps for staging deploy, or done)
        → DevOps (staging deploy + checks; prod prepared, human executes)
Security Reviewer: on referral, anywhere in the chain; their no is final.
```

### Model Policy
- **Claude Fable & Sonnet 4.6**: Used for deep thinking and deep implementation. Fable handles the deepest work (architecture, ambiguity, cross-cutting) and Tech Lead duties. Sonnet 4.6 handles frequent-deep work like code review, preserving the shared Anthropic cap.
- **Codex**: Used for well-specified implementation and roles needing cap-proof reliability (e.g., conductor/Delivery Lead, and understudies for Claude roles to handle cap overflow).
- **Gemini (pro)**: Used for direction and PM-shaped thinking (Product Owner, UX/UI design, Technical Writer).

Cross-provider review is deliberate: Codex-implemented work is reviewed by
a Claude agent, mirroring the practice described in the Arcus Q&R.

## Human gates: the DEFAULT Ada interaction model

This is the standard model that applies to all client engagements and is the
model carried over when exporting workspace templates (ADA-30):

- Humans set boundaries and standards.
- Top-level issues need human sign-off before build starts (Product Owner
  and Tech Lead prepare; the human approves).
- Humans review at defined workflow points.
- Production deploys, and scope, cost, or client-facing calls are always
  human-owned, per the vision documents.

### The ADA-1 Exception (Ada Bootstrapping only)

This specific project (Ada Bootstrapping, platform build) runs autonomously
under the ADA-1 exception, granted for time pressure, low risk, and no
client exposure. It does **not** extend to any other project, workspace,
or template export (ADA-30 exports always carry the default model).

Under this exception:
- Agents have standing build authorisation without prior human sign-off.
- The agent review chain acts as the primary quality gate.
- Risky moves rely on strict backup and rollback discipline.

**Mandatory Escalation List (applies even under the exception):**
- Any public Multica or IP exposure.
- Destructive operations without a workable backup/rollback path.
- New spend or credentials.

## Lessons carried over from Arcus FM

- A single shared house-rules skill beats repeating conventions per agent.
- Explicit per-role review protocols ("when done, set X and reassign Y")
  are what make hand-offs reliable — never leave finished work unassigned.
- Casting-vote assignments per domain (technical → Tech Lead, product →
  PO, ship/no-ship → QA, security → veto) resolve deadlocks without humans.
- Backlog status = parked (no triggers); todo = an assigned agent fires.
  Use backlog for staged work, promote deliberately.
- Autopilots work as orchestration ticks (wave-manager pattern): cap
  concurrent chains, never re-trigger in-flight work, report to one issue.
