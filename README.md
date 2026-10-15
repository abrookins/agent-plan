# agent-plan

`agent-plan` is a filesystem-backed planning tool for agents and teams. It
stores shared plan state in a workspace-local `.agent-plan/` directory and
exposes the same workflow through a CLI, an MCP stdio server, and an importable
Go package.

The current Go implementation supports:

- multiple named plans per workspace
- ordered tasks with hierarchy and dependencies
- claim-aware task selection and claiming
- explicit whole-plan state loading with `show`
- transactional bulk task import/sync with dry-run validation
- compact `stats`, `audit`, and stale-lock inspection commands
- JSON output for agent-facing commands

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
go build -o "dist/agent-plan-$(go env GOOS)-$(go env GOARCH)" ./cmd/agent-plan
```

## Workspace Layout

```text
.agent-plan/
  workspace.json
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

- `workspace.json` tracks the store version and workspace-level metadata.
- `plan.json` stores plan metadata such as name, description, and spec path.
- `manifest.json` is the per-plan task summary index used for fast reads.
- `tasks/*.md` is the durable source of truth for active tasks.
- `tasks/*.claim` is the exclusive-create coordination primitive.
- `history/*.done` preserves completed task records.

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

Example `.gitignore` entries:

```gitignore
.agent-plan/plans/*/manifest.lock
.agent-plan/plans/*/tasks/*.claim
```

## CLI

Primary plan commands:

```bash
agent-plan init
agent-plan create "Go parity" --description "Track feature work"
agent-plan list
agent-plan get <plan-id> --json
agent-plan show <plan-id> --json
agent-plan show <plan-id> --mermaid
agent-plan update <plan-id> --spec-path docs/parity-design.md
agent-plan delete <plan-id> --yes
agent-plan add-task <plan-id> "Freeze parity matrix" --description "Capture the target surface"
agent-plan update-task <plan-id> <task-id> --status in_progress
agent-plan update-task <plan-id> <task-id> --status complete
agent-plan update-task <plan-id> <task-id> --result "Decision: ship. Evidence: tests pass. Handoff: review PR."
agent-plan delete-task <plan-id> <task-id>
agent-plan import <plan-id> --file plan-tasks.json --dry-run --json
agent-plan import <plan-id> --file plan-tasks.json --json
agent-plan chain <plan-id> <first-task-id> <second-task-id> <third-task-id> --json
agent-plan stats <plan-id> --json
agent-plan audit <plan-id> --json
agent-plan lock status <plan-id> --json
agent-plan lock clear <plan-id> --if-dead --json
agent-plan ready <plan-id> --json
agent-plan next <plan-id> --by <agent-id> --json
agent-plan claim-next <plan-id> --by <agent-id> --json
agent-plan install AGENTS.md
agent-plan mcp
agent-plan version
```

`show` is the explicit whole-plan snapshot command for agents that want the
current plan plus all tasks in one call. `get` returns the same plan payload and
remains available for parity with the existing surface. Both commands accept
`--mermaid` to print a Mermaid flowchart with task dependencies and hierarchy;
`--json` and `--mermaid` are mutually exclusive.

All commands accept `--dir <workspace-root>`. When `--dir` is omitted, the CLI
falls back to `AGENT_PLAN_DIR` before using the current working directory.
Agent-facing commands support `--json`. The `mcp` command does not because MCP
defines its own wire format.

`delete` prompts for confirmation unless `--yes` is passed or
`AGENT_PLAN_NONINTERACTIVE=1` is set.

Use task IDs when scripting agent workflows. Plan and task names are supported
only as exact matches; there is no fuzzy matching.

For large generated plans, prefer `import --dry-run` followed by `import` over
many individual `add-task` and `update-task` calls. Bulk import validates the
full graph before writing and matches existing tasks by stable `key`, so
rerunning the same input converges instead of duplicating tasks.

Minimal bulk import shape:

```json
{
  "tasks": [
    {
      "key": "design",
      "name": "Design the change",
      "description": "Choose the command and JSON contract"
    },
    {
      "key": "implement",
      "name": "Implement the change",
      "depends_on": ["design"]
    }
  ],
  "chains": [["design", "implement"]]
}
```

`tasks[].parent` and `tasks[].depends_on` may reference either task IDs or
stable import keys in the same payload. `chains` is a shorthand for adding
sequential dependency edges across existing IDs or import keys.

Task-level claim coordination still exists where it affects selection and
claiming. Use `next --by <id>` when claimer affinity matters; plain `next` does
not infer any external claimer context.

## Task Status

The CLI accepts these task statuses:

- `not_started`
- `in_progress`
- `blocked`
- `complete`
- `cancelled`

Mark finished work with `update-task --status complete`. Completed task records
move from `tasks/*.md` to `history/*.done`.

Use `update-task --result` to record a task's decision, evidence, and handoff.
The result remains available after completion, without a session transcript.

## Guidance Install

`install [AGENTS.md|CLAUDE.md|skill]` is onboarding/scaffolding, not plan
lifecycle state. It is intended for a human or setup step that wants managed
guidance files in a workspace or provider home.

## MCP

The stdio server exposes the full plan workflow:

- `plan_init`
- `plan_create`
- `plan_list`
- `plan_get`
- `plan_show`
- `plan_update`
- `plan_delete`
- `plan_add_task`
- `plan_update_task`
- `plan_delete_task`
- `plan_import`
- `plan_chain`
- `plan_stats`
- `plan_audit`
- `plan_lock_status`
- `plan_lock_clear`
- `plan_ready`
- `plan_next`
- `plan_claim_next`
- `plan_install`

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

### MCP argument validation

MCP tools reject missing required fields, unknown parameters, incorrect types,
and invalid task statuses before opening or creating a workspace. Discovery
publishes status choices for updates and filters: `not_started`, `in_progress`,
`blocked`, `complete`, and `cancelled`. Use `tools/list` to inspect the schema;
argument errors identify fields without echoing their values.
