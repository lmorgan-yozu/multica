# Projects and resources source map

- `server/cmd/multica/cmd_project.go` registers project `list`, `get`, `create`, `update`, `delete`, and `status`.
- The same file registers `project resource list/add/update/remove`.
- `server/cmd/multica/cmd_project_roadmap.go` registers `project roadmap`, wrapping `GET /api/projects/{id}/roadmap` (read-only projection: epics, milestones, dependency order, leaf progress).
- `server/cmd/multica/cmd_project_milestone.go` registers `project milestone list/add/update/remove`, wrapping `/api/projects/{id}/milestones`; `list` reads the roadmap projection's milestones array (there is no standalone milestone list endpoint).
- `server/cmd/multica/cmd_issue_roadmap.go` registers `issue milestone set/clear` (`PUT /api/issues/{id}/milestone`) and `issue dependency add/remove` (`POST/DELETE /api/issues/{id}/dependencies`); the server enforces same-project membership and rejects cycles at write time.
- The roadmap data model decision record is `docs/adr/0001-roadmap-data-model.md`.
- `project create --repo` attaches `github_repo` resources during project creation.
- `project resource add` supports shortcuts for `github_repo` (`--url`, `--default-branch-hint`) and `local_directory` (`--local-path`, `--daemon-id`, `--ref-label`), or generic `--ref '<json>'`.
- `project resource update` merges shortcut edits with existing `resource_ref` so a partial edit does not clobber required fields.
- `server/cmd/server/router.go` exposes `/api/projects` plus `/api/projects/{projectId}/resources` routes.
- `server/pkg/db/queries/project_resource.sql` is the CRUD query surface for `project_resource` rows.
- Project resources are written into `.multica/project/resources.json` for agent workdirs.
