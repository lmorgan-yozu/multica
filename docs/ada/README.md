# Ada — Yozu's delivery platform

Audience: anyone (human or agent) working on this fork.

Ada is Yozu's internal agentic delivery platform, built on this private fork
of the open-source Multica project (`github.com/lmorgan-yozu/multica`). The
open-source foundation handles scheduling, runtime, and agent lifecycle
management; everything Yozu layers on top — quality gates, structured
handoffs, spend monitoring, delivery metrics, skills, and workflows — is what
turns it into a platform for production client delivery.

Hard rules for this repository:

- All work stays local or in this private fork. **Never** push to, or open
  PRs against, the public Multica project (`github.com/multica-ai/multica`
  or any `multica-ai` remote).
- Feature branch + review flow; `main` moves only after review.
- The deployed instance (agents.tauntondene.io) runs this code — the
  platform you are modifying is the platform the team runs on. Migrations
  and deploys are production changes; staging first, prod is human-gated.

Contents:

- `VISION.md` — what Ada is meant to become, condensed from the June 2026
  vision documents (canonical copies are attached to issue ADA-1 in the Ada
  workspace).
- `GAP-ANALYSIS.md` — what the fork already has versus what the vision
  needs, mapped to the ADA backlog epics.
- `TEAM.md` — the agent team that builds Ada: roles, models, review chain,
  and escalation rules.
