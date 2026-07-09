---
name: agent-plan
description: Manage shared workspace plans with the agent-plan CLI and MCP server. Use when the user wants explicit shared planning, multi-agent coordination, machine-readable task state, or claim-aware task coordination.
---

# agent-plan

Use `agent-plan` when work should be tracked in a workspace-local
`.agent-plan/` store that multiple agents can read and update.

## When To Use It

- The user explicitly wants a shared plan or task list.
- Multiple agents need to coordinate work.
- The repo already contains a `.agent-plan/` directory.
- You need JSON or MCP output for plan and task state.

Do not use it for a one-off task unless the user wants explicit tracking.

## Core Workflow

### 1. Initialize or inspect the workspace

```bash
command -v agent-plan
agent-plan init --json
agent-plan list --json
agent-plan help show
```

- Use `agent-plan init` only when the workspace is not already initialized.
- Add `--dir <workspace-root>` when you are outside the target workspace.
- When `--dir` is omitted, the CLI falls back to `AGENT_PLAN_DIR` before using
  the current working directory.

### 2. Load the relevant plan

```bash
agent-plan list --json
agent-plan get <plan-id> --json
agent-plan show <plan-id> --json
agent-plan stats <plan-id> --json
agent-plan audit <plan-id> --json
agent-plan ready <plan-id> --json
```

- Prefer `show --json` or `get --json` for a cold start on a known plan.
- Prefer `stats --json` when you only need compact counts and readiness.
- Prefer `audit --json` when a plan may have bad edges, stale locks, or
  suspicious readiness.
- Prefer `ready --json` when you need actionable tasks only.

### 3. Create or update plan metadata

```bash
agent-plan create "Plan name" --description "Why this exists" --json
agent-plan update <plan-id> --name "New name" --json
agent-plan delete <plan-id> --yes --json
```

- Use `create` only when the user asked for a new plan or no suitable plan
  exists.
- `delete` prompts unless `--yes` is passed or
  `AGENT_PLAN_NONINTERACTIVE=1` is set.

### 4. Coordinate claim-specific work

```bash
agent-plan next <plan-id> --by <agent-id> --json
agent-plan claim-next <plan-id> --by <agent-id> --json
```

- Use `claim-next` when you need exclusive ownership before editing files.
- Use `next --by <agent-id>` when claimer affinity matters; plain `next` does
  not infer any external claimer context.

### 5. Update the plan as work changes

```bash
agent-plan add-task <plan-id> "Task title" --description "Why it is needed" --json
agent-plan update-task <plan-id> <task-id> --status in_progress --json
agent-plan update-task <plan-id> <task-id> --status complete --json
agent-plan update-task <plan-id> <task-id> --status blocked --json
agent-plan update-task <plan-id> <task-id> --parent <parent-task-id> --json
agent-plan update-task <plan-id> <task-id> --depends-on <other-task-id> --json
agent-plan update-task <plan-id> <task-id> --clear-dependencies --json
agent-plan update-task <plan-id> <task-id> --clear-dependencies --depends-on <replacement-task-id> --json
agent-plan chain <plan-id> <first-task-id> <second-task-id> <third-task-id> --json
agent-plan delete-task <plan-id> <task-id> --json
```

- Use `add-task` for newly discovered work in the same plan.
- Use `--status complete` when work is done so the durable record moves into
  `history/*.done`.
- Use `--depends-on` for blocking relationships.
- Use `--clear-dependencies --depends-on <id>` to replace dependency edges in
  one command.
- Use `chain` when several existing tasks should execute in sequence.
- Use `--parent` only when you want hierarchy or subtasks.
- Prefer exact task IDs in agent workflows. Name resolution is exact, not fuzzy.

## Creating Plans From Specs

When turning a spec into an `agent-plan`, do not stop at a flat task inventory.
For large specs or generated plans, prefer one validated bulk import over many
individual task mutations.

Bulk import workflow:

```bash
agent-plan import <plan-id> --file tasks.json --dry-run --json
agent-plan import <plan-id> --file tasks.json --json
agent-plan stats <plan-id> --json
agent-plan audit <plan-id> --json
agent-plan ready <plan-id> --json
```

Bulk import accepts stable `key` values for reruns and `chains` for concise
sequential dependency edges:

```json
{
  "tasks": [
    {"key": "phase-1", "name": "Phase 1"},
    {"key": "phase-2", "name": "Phase 2"}
  ],
  "chains": [["phase-1", "phase-2"]]
}
```

Required workflow:

1. Read the source spec.
2. Create parent tasks for sections or phases only as hierarchy.
3. Create child tasks for each actionable spec item.
4. Add explicit `--depends-on` relationships for execution order:
   - Foundation/setup tasks come first.
   - Phase N tasks depend on the relevant Phase N-1 completion gates.
   - Component/page migrations depend on shared asset, token, and helper tasks.
   - Verification and acceptance tasks depend on the implementation tasks they
     validate.
5. After creating tasks, audit the structure:
   - Count tasks with `parent_id`.
   - Count tasks with `depends_on`.
   - Run `agent-plan stats <plan-id> --json`.
   - Run `agent-plan audit <plan-id> --json`.
   - Run `agent-plan ready <plan-id> --json`.
6. If most or all tasks are ready, either add missing dependencies or explain
   why the work is intentionally parallel.
7. Do not mark the planning task complete until hierarchy and dependency
   semantics have both been verified.

## Task Statuses

Valid task statuses are:

- `not_started`
- `in_progress`
- `blocked`
- `complete`
- `cancelled`

`update-task` can also change `--name`, `--description`, `--claimed-by`,
`--clear-claimed-by`, `--parent`, `--depends-on`, `--clear-dependencies`, and
`--order`.

## Guidance Install

```bash
agent-plan install AGENTS.md --json
agent-plan install CLAUDE.md --json
agent-plan install skill --json
agent-plan install --global AGENTS.md --json
agent-plan install --global CLAUDE.md --json
agent-plan install --global skill --json
```

`install` is onboarding/scaffolding, not normal plan lifecycle state.

## Other Commands

```bash
agent-plan mcp
agent-plan version --json
agent-plan lock status <plan-id> --json
agent-plan lock clear <plan-id> --if-dead --json
agent-plan help <command>
agent-plan <command> --help
```

- `mcp` runs the stdio MCP server and does not use `--json`.
- `version --json` prints the CLI version.
- Use `lock clear --if-dead` only when `lock status` or `audit` shows local
  liveness evidence that the recorded owner process is gone.

## MCP Surface

Prefer MCP when the host agent already wants tool calls instead of shell calls.

Primary MCP tools:

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

## Notes

- Treat `tasks/*.md` and `history/*.done` as durable records.
- Treat `tasks/*.claim` and `manifest.lock` as coordination state.
- Prefer the CLI or MCP tools over editing `.agent-plan/` files by hand.
- Use `agent-plan help <command>` or `agent-plan <command> --help` when you
  need command-specific flag or environment details.
