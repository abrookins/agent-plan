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
agent-plan init
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
agent-plan ready <plan-id> --json
```

- Prefer `show --json` or `get --json` for a cold start on a known plan.
- Prefer `ready --json` when you need actionable tasks only.

### 3. Coordinate claim-specific work

```bash
agent-plan next <plan-id> --by <agent-id> --json
agent-plan claim-next <plan-id> --by <agent-id> --json
```

- Use `claim-next` when you need exclusive ownership before editing files.
- Use `next --by <agent-id>` when claimer affinity matters; plain `next` does
  not infer any external claimer context.

### 4. Update the plan as work changes

```bash
agent-plan add-task <plan-id> "Task title" --description "Why it is needed" --json
agent-plan update-task <plan-id> <task-id> --status in_progress --json
agent-plan update-task <plan-id> <task-id> --status complete --json
agent-plan update-task <plan-id> <task-id> --depends-on <other-task-id> --json
agent-plan delete-task <plan-id> <task-id> --json
```

- Use `add-task` for newly discovered work in the same plan.
- Use `--status complete` when work is done so the durable record moves into
  `history/*.done`.
- Use `--depends-on` for blocking relationships.
- Use `--parent` only when you want hierarchy or subtasks.
- Prefer exact task IDs in agent workflows. Name resolution is exact, not fuzzy.

## MCP Surface

Prefer MCP when the host agent already wants tool calls instead of shell calls.

Primary MCP tools:

- `plan_create`
- `plan_list`
- `plan_get`
- `plan_show`
- `plan_update`
- `plan_delete`
- `plan_add_task`
- `plan_update_task`
- `plan_delete_task`
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
