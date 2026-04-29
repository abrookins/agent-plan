---
name: agent-plan
description: Manage shared workspace plans with the agent-plan CLI and MCP server. Use when the user wants a repo-local task list, shared planning across agents, task claiming, or machine-readable plan state. Trigger phrases include "agent-plan", "shared plan", "claim the next task", "mark this done", "block this task", and "task list".
---

# agent-plan

Use `agent-plan` when work should be tracked in a workspace-local `.agent-plan/`
directory that multiple agents can read and update.

## When To Use It

- The user explicitly wants a shared plan or task list.
- Multiple agents or sessions need to coordinate work.
- The repo already contains a `.agent-plan/` directory.
- You need machine-readable task state through `--json` or MCP tools.

Do not use it for a one-off task unless the user wants explicit tracking.

## Core Workflow

### 1. Check or initialize the workspace

```bash
command -v agent-plan
agent-plan plan show --json
agent-plan init
```

- Use `agent-plan init` only when the workspace does not already have
  `.agent-plan/`.
- Add `--dir <workspace-root>` when you are outside the target workspace.

### 2. Inspect current work

```bash
agent-plan list --json
agent-plan next --json
agent-plan plan show --json
```

- Prefer `plan show --json` for a cold start.
- Prefer `next --json` when you want the next ready task.

### 3. Claim work before changing it

```bash
agent-plan claim <task-id> --json
```

- Claims are coordinated through exclusive creation of `tasks/*.claim`.
- If a task is already claimed, pick different work or ask the user how to
  resolve contention.

### 4. Update task state as work progresses

```bash
agent-plan add "Task title" --description "Why it is needed" --json
agent-plan block <task-id> --reason "What is blocking it" --json
agent-plan done <task-id> --output "Result or artifact" --json
```

- Use `add` for newly discovered work that belongs in the same plan.
- Use `block` instead of leaving work ambiguous when progress is stopped.
- Use `done` when the task is complete and you can record the outcome.

## JSON Preference

When an agent needs to consume command output programmatically, prefer:

- `agent-plan list --json`
- `agent-plan next --json`
- `agent-plan claim <id> --json`
- `agent-plan done <id> --json`
- `agent-plan block <id> --json`
- `agent-plan plan show --json`

The `mcp` subcommand does not use `--json`; it speaks MCP over stdio.

## MCP Surface

The MCP server exposes:

- `plan_init`
- `plan_add`
- `plan_list`
- `plan_next`
- `plan_claim`
- `plan_done`
- `plan_block`
- `plan_show`

Use MCP when the host agent already prefers MCP tool calls over shell commands.

## Notes

- Treat `tasks/*.md` as the durable task record.
- Treat `tasks/*.claim` and `manifest.lock` as coordination state.
- Prefer the CLI or MCP tools over editing `.agent-plan/` files by hand.
