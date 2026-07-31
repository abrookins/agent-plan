---
name: agent-plan
description: Manage shared workspace plans with the agent-plan CLI and MCP server. Use when the user wants explicit shared planning, multi-agent coordination, machine-readable task state, plan-store diagnostics, or claim-aware task coordination.
---

# agent-plan

Use `agent-plan` when work should be tracked in a workspace-local
`.agent-plan/` store that multiple agents can read and update.

## When To Use It

- The user explicitly wants a shared plan or task list.
- Multiple agents need to coordinate work.
- The repository already contains a `.agent-plan/` directory.
- You need bounded JSON or MCP access to plan and task state.
- You need to diagnose plan health, stale coordination state, or invalid edges.

Do not use it for a one-off task unless the user wants explicit tracking.

## Operating Contract

Once the target workspace is known, resolve its canonical absolute path and pass
it explicitly as `--dir /absolute/workspace` on every CLI call. This prevents
commands from silently using `AGENT_PLAN_DIR` or the wrong current directory.
MCP callers pass the same path as `root`.

Treat these files differently:

- `tasks/*.md` and `history/*.done` are durable task records.
- `tasks/*.claim` and `manifest.lock` are coordination state.
- The CLI or MCP server owns all plan files; do not edit them by hand.

For routine work, keep reads bounded. Full-plan snapshots can contain every task
description and output, so request them only when the complete graph is
necessary.

## Bounded Cold Start

Use this sequence when resuming or joining a known plan:

1. Run `stats` to learn the plan version, counts, readiness, claims, and health.
2. Run `audit` only if health is not `healthy`, readiness looks implausible, a
   lock or claim may be stale, or another coordination symptom is present.
3. Run `ready` or `next` to identify actionable work.
4. Run `get-task` for the chosen task and its direct dependencies.
5. Use `show` or `get` only when the whole task graph is explicitly needed.

```bash
agent-plan stats PLAN_ID --dir /absolute/workspace --json
agent-plan ready PLAN_ID --dir /absolute/workspace --json
agent-plan next PLAN_ID --dir /absolute/workspace --by AGENT_ID --json
agent-plan get-task PLAN_ID TASK_ID DEPENDENCY_ID \
  --dir /absolute/workspace \
  --fields id,name,status,depends_on,claimed_by,description \
  --json
```

If `stats.health` is `degraded` or `unreadable`, or coordination looks
suspicious, inspect diagnostics separately:

```bash
agent-plan audit PLAN_ID --dir /absolute/workspace --json
agent-plan lock status PLAN_ID --dir /absolute/workspace --json
```

Do not begin a cold start with `show --json` or `get --json` merely to discover
the next task.

## Progressive Task Retrieval

### CLI

`get-task` retrieves selected task records without loading a full plan snapshot:

```bash
agent-plan get-task PLAN_ID TASK_ID \
  --dir /absolute/workspace \
  --fields id,name,status,parent_id,depends_on,claimed_by,description \
  --limit 25 \
  --json
```

Task references resolve by ID first and exact name second. With no task
references, provide at least one filter:

```bash
agent-plan get-task PLAN_ID \
  --dir /absolute/workspace \
  --status in_progress \
  --claimed-by AGENT_ID \
  --fields id,name,status,depends_on,description_preview,description_bytes \
  --limit 25 \
  --json
```

Supported filters and controls are:

- one or more task references
- `--status STATUS`
- `--parent TASK`
- `--claimed-by ID`
- `--fields FIELD,FIELD,...`
- `--limit N`, from 1 through 100; the default is 25
- `--cursor CURSOR`
- `--known-version N`

The default fields are `id`, `name`, `status`, `parent_id`, `depends_on`,
`assignee`, `claimed_by`, `updated_at`, `description_preview`,
`description_bytes`, `output_preview`, and `output_bytes`. Previews are at most
240 Unicode code points.

Request full `description`, `output`, `path`, `claim_path`, or complete timestamp
fields only when needed. Other selectable fields include `sequence`,
`import_key`, `order`, `blocked_reason`, `created_at`, `claimed_at`,
`completed_at`, and `archived_at`.

`--json` emits compact JSON followed by one newline. Add `--pretty` only for
human inspection; it requires `--json` and uses two-space indentation.

### MCP

Use `plan_get_tasks` for the same bounded operation:

```json
{
  "plan": "PLAN_ID",
  "tasks": ["TASK_ID", "DEPENDENCY_ID"],
  "fields": ["id", "name", "status", "depends_on", "description"],
  "limit": 25,
  "known_version": 391,
  "root": "/absolute/workspace"
}
```

`tasks`, `status`, `parent`, `claimed_by`, `fields`, `limit`, `cursor`, and
`known_version` are optional, but the request must contain at least one task
reference or filter.

Read the canonical result from `structuredContent`. It reports `plan_id`,
`plan_name`, `version`, `unchanged`, `returned`, `next_cursor`, `fields`, and
`tasks`. The text content is only a short summary.

### Pagination and versions

Cursors are opaque and bind the plan ID, plan version, normalized filters,
requested fields, and last ordering key. If the plan changes between pages, a
cursor returns the typed `cursor_stale` error. Restart the query from the first
page with no cursor; never combine pages from different versions.

`plan_stats`, `plan_ready`, `plan_get_tasks`, `plan_get`, and `plan_show` accept
`known_version`. When that value still matches, the result is only:

```json
{"plan_id":"PLAN_ID","version":391,"unchanged":true}
```

No task records are loaded for an unchanged result. Use this for polling rather
than repeatedly retrieving identical task data.

### Full-plan reads

`get` and `show` retain whole-plan semantics and can be large:

```bash
agent-plan show PLAN_ID --dir /absolute/workspace --json
agent-plan get PLAN_ID --dir /absolute/workspace --json
```

Prefer `stats`, `ready`, or `get-task` unless the operation truly requires the
complete graph. Mermaid output remains an explicit compact topology view:

```bash
agent-plan show PLAN_ID --dir /absolute/workspace --mermaid
```

## Claim and Mutation Workflow

Use `claim-next` when exclusive ownership is needed before editing files. Use
`next --by` when claimer affinity matters without changing state.

```bash
agent-plan claim-next PLAN_ID \
  --dir /absolute/workspace \
  --by AGENT_ID \
  --json
```

Apply exactly one mutation per tool call. Do not place multiple mutation
commands in one shell invocation, and never pipe a mutation into `jq` or another
process. A pipe can hide the mutation result or make a successful write look
failed. Let the command return its own result, then verify with a separate
read-only call.

Before changing task state, read the current plan version or the targeted task.
After a material mutation, use a separate `get-task`, `stats`, or `audit` call:

```bash
agent-plan update-task PLAN_ID TASK_ID \
  --dir /absolute/workspace \
  --status in_progress \
  --json
```

Then, in a new tool call:

```bash
agent-plan get-task PLAN_ID TASK_ID \
  --dir /absolute/workspace \
  --fields id,name,status,claimed_by,updated_at \
  --json
```

Other mutations follow the same one-call pattern:

```bash
agent-plan add-task PLAN_ID "Task title" \
  --dir /absolute/workspace \
  --description "Why it is needed" \
  --json

agent-plan update-task PLAN_ID TASK_ID \
  --dir /absolute/workspace \
  --depends-on OTHER_TASK_ID \
  --json

agent-plan update-task PLAN_ID TASK_ID \
  --dir /absolute/workspace \
  --clear-dependencies \
  --depends-on REPLACEMENT_TASK_ID \
  --json

agent-plan delete-task PLAN_ID TASK_ID \
  --dir /absolute/workspace \
  --json
```

Valid task statuses are `not_started`, `in_progress`, `blocked`, `complete`,
and `cancelled`. Completing a task moves its durable record to
`history/*.done`.

## Creating Plans From Specs

When turning a spec into a plan, create parent tasks for sections or phases,
child tasks for every actionable item, and explicit dependency edges for
execution order. Parent-child nesting alone does not affect readiness.

For a large plan, prefer one validated import over many individual mutations:

```bash
agent-plan import PLAN_ID \
  --dir /absolute/workspace \
  --file /absolute/workspace/.ai/tasks.json \
  --dry-run \
  --json
```

Apply the import in a separate tool call after inspecting the preview:

```bash
agent-plan import PLAN_ID \
  --dir /absolute/workspace \
  --file /absolute/workspace/.ai/tasks.json \
  --json
```

Then validate in separate calls:

```bash
agent-plan stats PLAN_ID --dir /absolute/workspace --json
agent-plan audit PLAN_ID --dir /absolute/workspace --json
agent-plan ready PLAN_ID --dir /absolute/workspace --json
```

Use stable import keys for reruns and `chains` for concise sequential
dependencies. Do not complete the plan-creation task until hierarchy,
dependency semantics, health, and initial readiness have been checked.

## Dashboard Session Binding

External `agent-plan` state does not bind an Agent Dashboard session. When work
is happening in the dashboard, inspect its compact self-context and read the
canonical envelope from `structuredContent.data`:

```javascript
const response =
  await tools.mcp__agent_dashboard__dashboard_get_self_context({});
const context = response.structuredContent.data;
const boundPlan = context.session.task_list_id;
```

If the session is unbound or points to a different plan, call
`dashboard_self_set_plan_id` with the intended plan ID, then verify with a
second compact self-context read. Do this before or while claiming the first
task.

Compact detail is the default. Request only sections needed for the current
decision:

```javascript
const response =
  await tools.mcp__agent_dashboard__dashboard_get_self_context({
    detail: "compact",
    include: ["relationships"]
  });
const context = response.structuredContent.data;
```

Optional includes are `provider_features`, `runtime`, `queue_messages`,
`loop_configuration`, `peers`, and `relationships`. Use `detail: "full"` only
when all optional sections are genuinely required.

The dashboard response envelope is `{ok, operation, target, data, warnings}`.
Payload fields live only under `data`. Do not read top-level compatibility
aliases and do not request the legacy response mode in examples or workflows.

## Output Budgets and Error Handling

Default structured-content budgets are:

| Operation | Budget |
|---|---:|
| `plan_stats` | 2 KiB |
| healthy `plan_audit` | 2 KiB |
| first `plan_ready` page | 32 KiB |
| first `plan_get_tasks` page | 64 KiB |
| compact dashboard self-context | 4 KiB |
| MCP text content | 512 UTF-8 bytes |

If selected fields would exceed a bounded budget, fewer items are returned with
`next_cursor`. An individually requested field may exceed the budget because
the caller explicitly opted into it; the response reports its byte count.
`plan_get` and `plan_show` are intentionally exempt full reads.

Handle typed errors by code:

- `invalid_argument`
- `plan_not_found`
- `plan_unreadable`
- `task_not_found`
- `task_record_missing`
- `path_outside_plan`
- `manifest_invalid`
- `cursor_stale`

Do not infer success from partial text. CLI JSON errors use
`{"ok":false,"error":{...}}`; MCP callers inspect the structured error result.
For `cursor_stale`, restart pagination. For store-integrity or path errors, stop
mutating and run the smallest applicable diagnostic read.

## Compatibility and Migration

- Manifest version 1 remains readable. The first successful mutation upgrades
  that plan to version 2; read-only calls do not rewrite it.
- `get`, `show`, and existing mutation results retain their semantic fields.
- Compact CLI JSON changes whitespace only. Use `--pretty --json` when a person
  needs indented output.
- MCP `structuredContent` is canonical. MCP text is a bounded summary and no
  longer duplicates serialized JSON.
- `plan_get_tasks`, pagination, field selection, and `known_version` are
  additive.
- A cursor from an older plan version fails with `cursor_stale`; restart rather
  than mixing versions.
- A matching `known_version` returns `unchanged: true` without task records.
- Dashboard top-level compatibility aliases last for one release only. New
  guidance and consumers read `structuredContent.data`; do not opt into aliases.

## Guidance Installation and Other Commands

Installation is onboarding or scaffolding, not normal plan lifecycle work:

```bash
agent-plan install AGENTS.md --dir /absolute/workspace --json
agent-plan install CLAUDE.md --dir /absolute/workspace --json
agent-plan install skill --dir /absolute/workspace --json
agent-plan install --global skill --json
```

Other useful commands:

```bash
agent-plan list --dir /absolute/workspace --json
agent-plan version --json
agent-plan mcp
agent-plan help get-task
agent-plan help COMMAND
```

`mcp` runs the stdio server and does not use `--json`.

## Completion Gate

Before reporting coordinated work complete:

- Confirm the canonical workspace path and plan ID.
- Confirm the dashboard binding when a dashboard session is active.
- Confirm every mutation ran alone and was followed by a separate verification.
- Confirm claimed work reached its intended status.
- Run `stats`; run `audit` if health or coordination is suspicious.
- Check `ready` so unfinished actionable work is not overlooked.
- For skill changes, use the supported Upskill draft, render, diff or rebase,
  dry-run promotion, and promotion workflow. Never edit rendered agent
  directories or generated skill output directly.
