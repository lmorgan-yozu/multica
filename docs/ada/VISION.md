# The Ada vision (June 2026)

Audience: agents and engineers working on this fork. Condensed from "AI at
Yozu — June 2026", the AI Capability Briefs, and the Arcus Q&R document —
the canonical copies are attached to issue ADA-1 in the Ada workspace.

## What Ada is

A managed, multi-agent delivery platform where coding agents work alongside
their human counterparts: they claim tickets, work in parallel, hand off
context across sessions, and operate under closer review discipline than a
traditional team — with humans as the arbiters of architecture, scope, and
merges. A small senior team plus an agent fleet delivers what previously
took multiple squads, at 50–60% of equivalent T&M cost, with quality going
up, not down.

## The pillars

1. **Role-specific agents.** Every human role on a project has agent
   counterparts (backend, frontend, design, PM, QA, …), each with a system
   prompt built for the role, layered on team- and project-level skills.

2. **Skills library.** 80+ skills encode Yozu's 13 years of delivery
   practice. Skills carry transferable know-how only; client code and domain
   data stay inside the client's project. Skills are versioned, reviewed,
   and compound with every engagement.

3. **Structured handoffs.** When an agent session ends, it records what's
   done, what's remaining, decisions made, and uncertainties; the next agent
   resumes with full context. Without this, agents repeat work, loop, or
   make conflicting decisions. One of the hardest problems solved; core IP.

4. **Workflow orchestration.** Tickets route automatically between role
   agents (backend → frontend → QA), context passed at each transition,
   with human review points at defined stages.

5. **Quality gates.** The agent that writes code never reviews its own
   work. Every change gets at least one independent AI review pass (often a
   different model provider) before human review. Implementer ≠ approver,
   by construction. PR rejection rate, comments per PR, coverage, and
   defect escape rate are continuously monitored with an escalation path.

6. **Failure detection.** Agents loop, drift, and burn tokens without
   progress. Ada watches for climbing spend without meaningful progress,
   flags it, and hits the brakes when thresholds are exceeded. Context
   windows are managed; sessions are kept short and focused.

7. **Observability.** Every agent action — file changes, tool calls,
   decisions, tokens spent — is visible in real time. Reviewing an agent is
   reviewing a transparent timeline, not auditing a black box.

8. **Model agnosticism.** Per-role model selection across frontier and
   open-source models (e.g. high-capability models for architectural work,
   implementation-tuned models for clear tickets). Knowing which model to
   point at which problem is part of the encoded experience.

9. **Agent memory** (in development). Long-term, shared-access memory that
   lets agents traverse team- and project-level knowledge, making them more
   predictable and deterministic.

10. **AI-native PM and QA.** A versioned layer of workflows spanning
    planning & estimation, sprint planning, RAID tracking, budget burn,
    change control, status reporting, ceremonies, release readiness, QA
    evidence packs, and test authoring — with the PM/QA as the authority at
    every step.

## Where humans stay in charge

Strategic and foundational architecture decisions; ambiguous requirements;
commercial context; scope, cost, and client-relationship calls; production
deploys; final review and acceptance. AI handles the operational load — the
humans handle everything that actually matters.

## Commercial boundary (from the Arcus Q&R)

Handed to clients: source code, project-level coding standards, project
skill files, docs, test suite — everything in the project repo. Retained as
Yozu IP: this delivery platform, the orchestration layer, agent harnesses,
and the cross-project skills library. Protect that boundary in everything
built here.
