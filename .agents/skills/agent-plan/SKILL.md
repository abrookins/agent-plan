---
name: agent-plan
description: Manage shared workspace plans with the agent-plan CLI and MCP server. Use when the user wants explicit shared planning, multi-agent coordination, machine-readable task state, or session-aware task claiming.
---

# agent-plan

Use `agent-plan` when work should be tracked in a workspace-local
`.agent-plan/` store that multiple agents or sessions can read and update.

## When To Use It

- The user explicitly wants a shared plan or task list.
- Multiple agents or sessions need to coordinate work.
- The repo already contains a `.agent-plan/` directory.
- You need JSON or MCP output for plan and task state.

Do not use it for a one-off task unless the user wants explicit tracking.

## Core Workflow

### 1. Initialize or inspect the workspace

```bash
command -v agent-plan
agent-plan init
agent-plan list --json
```

- Use `agent-plan init` only when the workspace is not already initialized.
- Add `--dir <workspace-root>` when you are outside the target workspace.

### 2. Load the relevant plan

```bash
agent-plan list --json
agent-plan get <plan-id> --json
agent-plan ready <plan-id> --json
```

- Prefer `get --json` for a cold start on a known plan.
- Prefer `ready --json` when you need actionable tasks only.

### 3. Bind or claim session-specific work

```bash
agent-plan bind-current <plan-id> --json
agent-plan bind-session <plan-id> --session <session-id> --json
agent-plan next <plan-id> --session <session-id> --json
agent-plan claim-next <plan-id> --session <session-id> --json
```

- Use `bind-current` when the current runtime already exposes a session ID in
  environment variables.
- Use `claim-next` when you need exclusive ownership before editing files.

### 4. Update the plan as work changes

```bash
agent-plan add-task <plan-id> "Task title" --description "Why it is needed" --json
agent-plan update-task <plan-id> <task-id> --status in_progress --json
agent-plan update-task <plan-id> <task-id> --status complete --json
agent-plan update-task <plan-id> <task-id> --depends-on <other-task-id> --json
agent-plan delete-task <plan-id> <task-id> --json
```

- Use `add-task` for newly discovered work in the same plan.
- Use `--depends-on` for blocking relationships.
- Use `--parent` only when you want hierarchy or subtasks.

## MCP Surface

Prefer MCP when the host agent already wants tool calls instead of shell calls.

Primary MCP tools:

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

## Notes

- Treat `tasks/*.md` and `history/*.done` as durable records.
- Treat `tasks/*.claim`, `manifest.lock`, and `sessions/*.json` as coordination state.
- Prefer the CLI or MCP tools over editing `.agent-plan/` files by hand.
