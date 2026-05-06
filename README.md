# agent-plan

`agent-plan` is a filesystem-backed planning tool for agents and teams. It
stores shared plan state in a workspace-local `.agent-plan/` directory and
exposes the same workflow through a CLI, an MCP stdio server, and an importable
Go package.

The current Go implementation supports:

- multiple named plans per workspace
- ordered tasks with hierarchy and dependencies
- session-aware task selection and claiming
- JSON output for agent-facing commands
- compatibility aliases for the earlier default-plan workflow

## Install

From the repo root:

```bash
make install
```

That installs `agent-plan` into `$(go env GOBIN)` when `GOBIN` is set, or
`$(go env GOPATH)/bin` otherwise.

## Build

```bash
make build
make build-all
make test
make clean
```

The direct Go build entry point is:

```bash
go build -o dist/agent-plan ./cmd/agent-plan
```

## Workspace Layout

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

- `workspace.json` tracks store version and the default plan used by legacy
  aliases such as `add`, `claim`, `done`, `block`, and `plan show`.
- `plan.json` stores plan metadata such as name, description, spec path, and
  scoped session IDs.
- `manifest.json` is the per-plan task summary index used for fast reads.
- `tasks/*.md` is the durable source of truth for active tasks.
- `tasks/*.claim` is the exclusive-create coordination primitive.
- `history/*.done` preserves completed task records.
- `sessions/*.json` stores local session binding snapshots.

Existing single-plan `.agent-plan/manifest.json` workspaces are migrated into
the new multi-plan layout automatically on first open.

## Git

Do not commit `.agent-plan/` by default. It often contains transient local
coordination state.

If a team wants repo-shared planning, commit the durable records:

- `.agent-plan/workspace.json`
- `.agent-plan/plans/*/plan.json`
- `.agent-plan/plans/*/manifest.json`
- `.agent-plan/plans/*/tasks/*.md`
- `.agent-plan/plans/*/history/*.done` when audit history belongs in Git

Do not commit the coordination files:

- `.agent-plan/plans/*/manifest.lock`
- `.agent-plan/plans/*/tasks/*.claim`
- `.agent-plan/sessions/*.json`

Example `.gitignore` entries:

```gitignore
.agent-plan/plans/*/manifest.lock
.agent-plan/plans/*/tasks/*.claim
.agent-plan/sessions/*.json
```

## CLI

Primary plan commands:

```bash
agent-plan init
agent-plan create "Go parity" --description "Track feature work"
agent-plan list
agent-plan get <plan-id> --json
agent-plan update <plan-id> --spec-path docs/parity-design.md
agent-plan delete <plan-id> --yes
agent-plan add-task <plan-id> "Freeze parity matrix" --description "Capture the target surface"
agent-plan update-task <plan-id> <task-id> --status in_progress
agent-plan delete-task <plan-id> <task-id>
agent-plan ready <plan-id> --json
agent-plan next <plan-id> --session <session-id> --json
agent-plan claim-next <plan-id> --session <session-id> --json
agent-plan bind-session <plan-id> --session <session-id>
agent-plan bind-current <plan-id>
agent-plan install AGENTS.md
agent-plan mcp
```

Compatibility aliases for the workspace default plan:

```bash
agent-plan add "Task title" --description "Why it is needed"
agent-plan claim <task-id>
agent-plan done <task-id> --output "PR #47"
agent-plan block <task-id> --reason "Waiting on another change"
agent-plan list-tasks
agent-plan plan show
```

All commands accept `--dir <workspace-root>`. Agent-facing commands support
`--json`. The `mcp` command does not because MCP defines its own wire format.

## MCP

The stdio server exposes the full plan workflow:

- `plan_init`
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

It also keeps compatibility helpers for the default-plan workflow:

- `plan_add`
- `plan_claim`
- `plan_done`
- `plan_block`
- `plan_show`

Example MCP config:

```json
{
  "mcpServers": {
    "agent-plan": {
      "command": "/absolute/path/to/agent-plan",
      "args": ["mcp"]
    }
  }
}
```

## Docs

- `docs/parity-design.md` documents the filesystem-backed parity design
- `docs/manual-qa.md` defines the manual QA plan for the Go binary

## Repo Skill

The repo includes a local skill at `.agents/skills/agent-plan/SKILL.md` for
agents that need a compact `agent-plan` workflow.
