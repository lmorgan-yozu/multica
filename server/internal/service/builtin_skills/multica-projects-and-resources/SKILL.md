---
name: multica-projects-and-resources
description: Use when creating, inspecting, updating, or debugging Multica projects and project resources. Covers durable project context, github_repo and local_directory resources, how resources affect future agent task context, when to bind repos, and when not to mutate resources.
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Projects and Resources

## Quick start

Projects are durable context containers. Resources attached to a project can affect future agent tasks.

```bash
multica project list --output json
multica project get <project-id> --output json
multica project resource list <project-id> --output json
```

Project resources are mutated through project resource commands/endpoints. Issue
comments do not create durable project resources.

## Core model

A project groups work and carries durable resources. A resource is not just display metadata; it is context later injected into task briefs and `.multica/project/resources.json`.

Common resource types:

- `github_repo` — durable GitHub repo context, with `resource_ref.url` and optional `default_branch_hint`;
- `local_directory` — daemon-local path context, with `resource_ref.local_path`, `daemon_id`, and optional label.

## CLI

```bash
multica project list --output json
multica project get <project-id> --output json
multica project roadmap <project-id> --output json
multica project create --title "<title>" --repo <github-url> --output json
multica project update <project-id> --title "<title>" --output json
multica project status <project-id> in_progress --output json
multica project resource list <project-id> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --output json
multica project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --output json
multica project resource update <project-id> <resource-id> --url <new-github-url> --output json
multica project resource remove <project-id> <resource-id> --output json
```

Use `--ref '<json>'` only for resource types or payloads not covered by shortcuts.

`project roadmap` is a read-only projection of the project's issue graph:
top-level issues as epics in dependency order with done/total leaf progress,
plus milestone groupings. It returns the same shape as
`GET /api/projects/{id}/roadmap`; there is no roadmap state to hand-maintain.

The roadmap's durable inputs are written with these verbs:

```bash
multica project milestone list <project-id> --output json
multica project milestone add <project-id> --name "<name>" --target-date 2026-07-01 --output json
multica project milestone update <project-id> <milestone-id> --target-date 2026-08-01 --output json
multica project milestone remove <project-id> <milestone-id>
multica issue milestone set <issue-id> <milestone-id> --output json
multica issue milestone clear <issue-id> --output json
multica issue dependency add <issue-id> <depends-on-issue-id> --output json
multica issue dependency remove <issue-id> <depends-on-issue-id>
```

`issue dependency add A B` means A depends on B (B must land first). Both
issues must be in the same project; links that would close a cycle are
rejected. Issue ids accept routable keys (`MUL-123`); milestone ids accept a
full UUID or unique prefix (find them via `project milestone list`).

## When to add a resource

Add/update a project resource when the user asks for durable project context: "把这个 GitHub repo 绑到项目上", "以后都用这个 repo", "agent 总是拿不到这个项目的仓库", or "这个项目要在我的本地目录里跑".

Project resources are durable and affect future tasks. `multica repo checkout`
is task-local checkout state.

## Debugging wrong context

1. `multica project get <project-id> --output json`.
2. `multica project resource list <project-id> --output json`.
3. Check `github_repo.resource_ref.url`, `default_branch_hint`, and `local_directory.resource_ref.daemon_id`.
4. Updating resources is a durable mutation. After an update, listing the
   resource is the verification path.
5. If resources match the expected task context, inspect runtime/repo checkout
   path next.

## Side effects

Project create/update/delete/status and project resource add/update/remove mutate durable workspace state and affect future tasks. Ask before changing `local_directory` unless the user explicitly requested that exact local path.

More source-backed details: `references/projects-and-resources-source-map.md`.
