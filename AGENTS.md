# Repository Guidelines

This file provides guidance to AI agents when working with code in this repository.

> **Single source of truth:** This file is a concise pointer document.
> All authoritative architecture, coding rules, commands, and conventions
> live in **CLAUDE.md** at the project root. Read that file first.

## Quick Reference

### Architecture

Go backend + monorepo frontend (pnpm workspaces + Turborepo) with shared packages.

- `server/` — Go backend (Chi router, sqlc, gorilla/websocket)
- `apps/web/` — Next.js frontend (App Router)
- `apps/desktop/` — Electron desktop app
- `packages/core/` — Headless business logic (Zustand stores, React Query hooks, API client)
- `packages/ui/` — Atomic UI components (shadcn/Base UI, zero business logic)
- `packages/views/` — Shared business pages/components
- `packages/tsconfig/` — Shared TypeScript config

### State Management (critical)

- **React Query** owns all server state (issues, members, agents, inbox, workspace list)
- **Zustand** owns all client state (current workspace selection, view filters, drafts, modals)
- All Zustand stores live in `packages/core/` — never in `packages/views/` or app directories
- WS events invalidate React Query — never write directly to stores

### Package Boundaries (hard rules)

- `packages/core/` — zero react-dom, zero localStorage, zero process.env
- `packages/ui/` — zero `@multica/core` imports
- `packages/views/` — zero `next/*`, zero `react-router-dom`, use `NavigationAdapter` for routing
- `apps/web/platform/` — only place for Next.js APIs

### Commands

```bash
make dev              # Auto-setup + start everything
pnpm typecheck        # TypeScript check
pnpm test             # TS unit tests (Vitest)
make test             # Go tests
make check            # Full verification pipeline
```

See CLAUDE.md for the complete command reference.

## Multica Host Safety Rules

Provenance: copied verbatim from `/home/lmorgan/docker/multica/POSTMORTEM-2026-06-11.md` on 2026-06-11.

You are running *inside* the system you are editing. The production containers are your own control plane — if you take down `multica-postgres-1`, every agent task (including yours) loses the daemon API mid-flight.

1. **Never run `docker compose` from a multica repo checkout on this host.** Not `up`, not `down`, not `restart`, not `rm` — and not Makefile/package.json targets that wrap them. The repo's compose project is named `multica`, which collides with production. Any compose command in your checkout can adopt, recreate, or remove the live prod containers.

2. **Need a database for tests?** Start a throwaway container with a unique name and a unique high host port, e.g.:
   ```sh
   docker run -d --name <task-id>-pg -p 127.0.0.1:55<nnn>:5432 \
     -e POSTGRES_PASSWORD=test pgvector/pgvector:pg17
   ```
   (This is the existing `ada23-test-pg` / `ada52-merge-pg` pattern.) Remove it when your task finishes.

3. **Never bind these host ports:** 5432 and 3000 (freightsoft), 5437/8080/3030 (multica prod), 8081/3031 (multica staging), 9898 (backrest). Always pick a fresh 55xxx port and bind to 127.0.0.1.

4. **Hands off these containers:** `multica-postgres-1`, `multica-backend-1`, `multica-frontend-1`, and the `multica-staging-*` set. Deployments happen only via the documented staging→prod procedure driven from `/home/lmorgan/docker/multica`, never by ad-hoc compose/docker commands against running containers.

5. **When syncing upstream, audit for config-affecting changes** (new/renamed env vars, port or volume changes — e.g. upstream #1773) and update `/home/lmorgan/docker/multica/.env` and compose accordingly *before* a rebuilt image reaches prod.

6. **If prod postgres ever goes missing or stuck in `Created`:**
   ```sh
   docker rm multica-postgres-1
   cd /home/lmorgan/docker/multica && docker compose up -d postgres
   ```
   Data is safe in `./data/db`; the compose file there is the source of truth.
