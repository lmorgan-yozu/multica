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

## Model policy

- **Claude Fable** for the deepest thinking and implementation (architecture, ambiguity, cross-cutting work).
- **Claude Sonnet 4.6** for frequent-deep work like code review, preserving Fable's shared Anthropic cap.
- **Codex** for well-specified implementation and anything needing cap-proof reliability (conductor, understudies). When Claude caps are hit, work overflows to Codex understudies.
- **Gemini** for direction/PM-shaped thinking (product ownership, design direction, docs).

## Roster

| Role | Model / runtime | Purpose |
| --- | --- | --- |
| Product Owner | pro (Gemini) | Owns the backlog and acceptance against the vision docs; direction/PM-shaped thinking |
| UX/UI Designer | pro (Gemini) | Design direction; direction/PM-shaped thinking |
| Delivery Lead | gpt-5.5 (Codex) | Flow, risk, and status; squad leader; conductor for cap-proof reliability |
| Tech Lead | claude-fable-5 (Claude) | Technical direction and breakdown; casting vote on technical deadlocks; second-line on Fable |
| Tech Lead (Codex) | gpt-5.5 (Codex) | Understudy for Fable cap overflow |
| Backend Engineer | gpt-5.5 (Codex) | Go server, schema, API, daemon/CLI |
| Frontend Engineer | gpt-5.5 (Codex) | Next.js web app |
| Fable Engineer (HIGH COST) | claude-fable-5 (Claude) | Complex, ambiguous, cross-cutting work only — Tech Lead must justify its use |
| Code Reviewer | claude-sonnet-4-6 (Claude) | Primary quality gate; frequent-deep reviews, preserving Fable cap |
| Code Reviewer (Codex) | gpt-5.5 (Codex) | Understudy for Claude cap overflow |
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

Cross-provider review is deliberate: Codex-implemented work is reviewed by
a Claude agent, mirroring the practice described in the Arcus Q&R.

## Human gates

- Top-level issues need human sign-off before build starts (Product Owner
  and Tech Lead prepare; the human approves).
- Production deploys and destructive operations are never agent-executed.
- Scope, cost, client-facing, and IP matters always escalate to the human.

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
