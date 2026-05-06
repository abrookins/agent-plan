# Agent-Plan Go Parity Design

This document defines the Go repo's parity target relative to the Python `agent-plan`
package while keeping the Go repo's filesystem-native coordination model.

## Scope

The Go implementation should mirror the Python tool's user-facing capabilities:

- plan catalog operations: `create`, `list`, `get`, `update`, `delete`
- task operations: `add-task`, `update-task`, `delete-task`
- task coordination helpers: `ready`, `next`, `claim-next`
- session helpers: `bind-session`, `bind-current`
- guidance installation: `install AGENTS.md|CLAUDE.md|skill`
- MCP tools for the same plan and task workflows

The Go implementation does not need to copy the Python storage backend or rely
on any external session-management runtime.

## Design Constraints

- Storage stays filesystem-backed under a workspace-local `.agent-plan/`.
- Task claiming continues to use exclusive file creation as the coordination
  primitive.
- Task files remain the durable source of truth.
- Completed work remains archived in `history/`.
- The tool must not assume any external coordinator. Session binding is
  represented as local filesystem metadata.

## Filesystem Layout

```text
.agent-plan/
  workspace.json
  sessions/
    <session-id>.json
  plans/
    <plan-id>/
      plan.json
      manifest.json
      manifest.lock
      tasks/
        <task-id>.md
        <task-id>.claim
      history/
        <task-id>.done
```

Notes:

- `workspace.json` tracks store version and the default plan ID used by legacy
  single-plan commands.
- `plan.json` stores plan metadata such as `name`, `description`, `spec_path`,
  and scoped `session_ids`.
- `manifest.json` stores the task summary index for a single plan.
- `sessions/<session-id>.json` stores local session-to-plan binding metadata.

## Compatibility Strategy

### Python parity surface

The Go CLI should support the Python command set directly:

- `agent-plan create`
- `agent-plan list`
- `agent-plan get`
- `agent-plan update`
- `agent-plan delete`
- `agent-plan add-task`
- `agent-plan update-task`
- `agent-plan delete-task`
- `agent-plan ready`
- `agent-plan next`
- `agent-plan claim-next`
- `agent-plan bind-session`
- `agent-plan bind-current`
- `agent-plan install`

### Legacy Go aliases

The existing Go commands should remain as compatibility aliases where practical:

- `init`
- `add`
- `claim`
- `done`
- `block`
- `plan show`
- `mcp`

These commands operate on the workspace's default plan.

## Migration

If a workspace contains the legacy single-plan layout:

- `.agent-plan/manifest.json`
- `.agent-plan/tasks/`
- `.agent-plan/history/`

the first write against the new store migrates that data into a generated default
plan under `.agent-plan/plans/<default-plan-id>/` and writes `workspace.json`.

The migration must preserve:

- task IDs
- ordering
- dependencies
- completion history
- claim files

## Session Semantics

The Python version binds sessions through a separate runtime store. The Go repo
stores local session binding snapshots instead.

- `bind-session` writes `.agent-plan/sessions/<session-id>.json`
- `bind-current` resolves the current session from environment variables and
  writes the same binding file
- `claim-next --session <id>` both assigns the task to the session and claims it
  through the normal `.claim` file path
- `next --session <id>` prefers tasks already assigned to that session before
  considering unassigned ready tasks

## MCP Parity

The MCP server should expose the full plan surface with machine-readable outputs:

- `plan_create`
- `plan_list`
- `plan_get`
- `plan_update`
- `plan_delete`
- `plan_add_task`
- `plan_update_task`
- `plan_delete_task`
- `plan_ready`
- `plan_next`
- `plan_claim_next`
- `plan_bind_session`
- `plan_bind_current`
- `plan_install`

The existing MCP tools for `plan_init`, `plan_add`, `plan_claim`, `plan_done`,
`plan_block`, and `plan_show` remain as compatibility helpers over the default
plan workflow.
