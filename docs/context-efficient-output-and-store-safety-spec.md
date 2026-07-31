# Context-Efficient Output and Plan-Store Safety

- Status: Draft
- Date: 2026-07-30
- Primary repository: `agent-plan`
- Companion repository: `agent-dashboard`

## Decision

Agent-facing tools must return the smallest complete answer for the question
asked, without repeating the same structured data in text and JSON. Plan
mutations must also preserve every durable record when the same workspace is
reached through different absolute paths or symbolic links.

This specification makes four coordinated changes:

1. Store task and claim paths relative to the plan directory and treat
   filesystem identity, not path spelling, as authoritative.
2. Commit manifest changes before removing superseded records.
3. Add bounded task retrieval, field selection, pagination, and unchanged
   responses for agent loops.
4. Make structured content canonical in MCP responses and keep text content to
   a short human-readable summary.

The existing `show` and `get` commands remain explicit full-plan operations.
Agents should not need them for routine task selection, progress checks, or
small task lookups.

## Problem

### Oversized and duplicated responses

The current MCP success helper serializes the full return value into
`content[].text` and returns the same value in `structuredContent`. Dashboard
tools can add another copy by placing compatibility fields both at the top
level and under `data`. A caller that stringifies the raw tool result exposes
all copies to the model.

The live plan involved in the incident provides a useful baseline:

| Read | UTF-8 bytes |
|---|---:|
| `show --json` for 348 tasks | 388,001 |
| `stats --json` | 405 |
| `ready --json` | 1,649 |
| Two selected task records | 1,133 |

The dashboard session response was 3,100 bytes. The fields needed to identify
the session, workspace, plan binding, task, and loop state were 201 bytes.

The problem is not that full state can be requested. The problem is that full
state and duplicate envelopes are the default path to answer narrow questions.

### Path-alias data loss

The incident workspace was reachable as both:

```text
/Users/andrew.brookins/src/agent-skill-evals
/Users/andrew.brookins/src/tmp/skill-eval-system
```

The second path was a symbolic link to the first. Existing manifest entries
used one spelling. A later mutation opened the workspace through the other.

Persistence wrote records under the new spelling, compared the old and new
absolute path strings, and removed the old path. Both spellings referred to the
same physical file, so the cleanup removed the newly written record. Completing
one task left its new `history/*.done` record intact but removed the other 345
task and history records.

### Misleading mutation and error results

Two additional defects made recovery harder:

- Plan resolution discarded a task-file read error and reported `plan not
  found`.
- `AddTask` retained a pointer into a task slice across an in-place sort. The
  mutation created one task but could return an unrelated task that moved into
  the pointer's position.

These are correctness defects, not presentation problems. They are included
because the same agent workflow exposed them.

## Goals

- Prevent record loss when callers mix real paths, symbolic links, `--dir`,
  `AGENT_PLAN_DIR`, and inherited working directories.
- Ensure a committed manifest never references a record that persistence
  removed.
- Return the task that a mutation actually created or updated.
- Preserve the underlying cause when a plan is unreadable.
- Let an agent retrieve plan counts, ready work, or a small task set without
  loading every description and output.
- Bound every default MCP read other than the explicitly full `plan_get` and
  `plan_show` tools.
- Avoid returning the same semantic payload more than once in a tool result.
- Preserve current claim and dependency semantics.
- Preserve current full-plan CLI and MCP capabilities.

## Non-goals

- Moving plan storage to a database or remote service.
- Removing descriptions, outputs, history, or full-plan snapshots.
- Adding model-generated summaries to the storage layer.
- Changing dashboard session-to-plan binding.
- Changing claim exclusivity or task readiness rules.
- Recovering description text already lost before this change.
- Changing the in-progress Mermaid output work.

## Design Principles

1. **Durable records come first.** A failed cleanup may leave an orphan file;
   it must not leave a manifest entry whose record was deleted.
2. **Path spelling is not identity.** Store paths should survive workspace
   moves and alternate routes to the same directory.
3. **Progressive disclosure is explicit.** Counts come before task summaries;
   task summaries come before full descriptions and outputs.
4. **Structured content is canonical.** Text explains the result but does not
   serialize it again.
5. **Large data is opt-in.** Descriptions, outputs, absolute paths, full
   histories, and full plans require an explicit request.
6. **Errors preserve evidence.** A missing record is not a missing plan.

## Storage Contract

### Canonical workspace identity

`Open`, `OpenOrInit`, and `Init` must return a canonical `Store.Root`.

- Convert the selected root to an absolute path.
- Resolve symbolic links after the root exists.
- If initialization creates the root, create it before resolving links.
- If a not-yet-created child path must be accepted, resolve the longest
  existing ancestor and append the cleaned remainder.
- Reject roots whose canonical `.agent-plan` path escapes the selected
  workspace.

Opening the same store through two symbolic links must produce the same
canonical `Store.Root`.

### Manifest version 2

Manifest version 2 stores `path` and `claim_path` relative to the plan
directory:

```json
{
  "version": 2,
  "plan_id": "c23684f0",
  "tasks": [
    {
      "id": "348-run-approved-remediated-redis-insight-qualification",
      "path": "tasks/348-run-approved-remediated-redis-insight-qualification.md",
      "claim_path": "tasks/348-run-approved-remediated-redis-insight-qualification.claim"
    }
  ]
}
```

Allowed record paths are:

```text
tasks/<task-id>.md
tasks/<task-id>.claim
history/<task-id>.done
```

The loader must clean and join relative paths against the canonical plan
directory. It must reject an absolute version 2 path, `..` traversal, or any
resolved path outside the plan directory.

### Version 1 compatibility

Version 1 manifests with absolute paths remain readable.

For each legacy path, the loader must:

1. Resolve the path and canonical plan directory.
2. Verify that the record is inside the canonical plan directory.
3. Convert the path to its version 2 relative form in memory.

The next successful mutation rewrites the manifest as version 2. An explicit
read must not mutate the store.

### Safe commit order

Persistence must compute a change set instead of rewriting every task record.
For each mutation:

1. Load and validate the current manifest and affected durable records.
2. Capture stable task IDs before any operation that can reorder a slice.
3. Write new or changed record files with atomic rename.
4. Create claim files required by the new state. Do not remove superseded
   claims yet.
5. Atomically commit `plan.json` and the new manifest.
6. Remove superseded task, history, and claim files.

Steps 3 through 5 must not remove a path referenced by the old manifest. If
step 5 fails, the old manifest and all records it references remain readable.
New files may remain as orphans and must be discoverable by `audit`.

If cleanup after step 5 fails, the mutation remains committed and returns a
warning that names the orphan path. The CLI exits successfully because the
requested state is durable. `audit` must report the orphan.

Before removing a superseded path, persistence must prove that it does not
refer to the active destination. It must compare canonical paths and, when both
paths exist, filesystem identity. If identity cannot be established safely,
cleanup must leave the old path in place and return a warning.

### Mutation result identity

Mutation methods must keep immutable IDs across persistence:

```go
createdID := record.ID
// Persist may reorder plan.Tasks.
task := plan.GetTask(createdID)
```

No method may retain a pointer to a slice element across a call that can append,
sort, or replace that slice. This rule applies to add, update, import, chain,
claim, and completion paths.

## Read and Error Contract

### Typed errors

The package must distinguish:

- `invalid_argument`
- `plan_not_found`
- `plan_unreadable`
- `task_not_found`
- `task_record_missing`
- `path_outside_plan`
- `manifest_invalid`
- `cursor_stale`

Errors should carry the relevant plan ID, task ID, and safe relative path.
They must preserve the wrapped operating-system or parsing error.

Plan resolution may fall back from an ID lookup to exact-name lookup only when
the ID does not exist. It must return corruption, permission, parsing, and
missing-record errors immediately.

With `--json`, CLI failures must emit one compact error object and exit
nonzero:

```json
{"ok":false,"error":{"code":"task_record_missing","message":"task record is missing","plan_id":"c23684f0","task_id":"001-example","path":"history/001-example.done"}}
```

MCP failures must set `isError: true`, put the error object in
`structuredContent`, and use a short text message. The text must not serialize
the error object.

### Audit degraded stores

`audit` must start from `plan.json`, `manifest.json`, and directory listings.
It must not require every task record to load successfully.

At minimum it must report:

- manifest entries with missing records
- record paths outside the plan directory
- duplicate record paths
- task and history orphans
- orphan claim files
- claim files that disagree with manifest state
- invalid manifest version or shape

`stats` may rely on manifest summaries and should remain available when a task
record is missing. Its result must include:

```json
{
  "health": "healthy",
  "finding_count": 0
}
```

Possible health values are `healthy`, `degraded`, and `unreadable`.

## Progressive Task Retrieval

### New CLI command

Add a bounded `get-task` command:

```text
agent-plan get-task <plan> [<task> ...]
  [--status STATUS]
  [--parent TASK]
  [--claimed-by ID]
  [--fields FIELD,FIELD,...]
  [--limit N]
  [--cursor CURSOR]
  [--known-version N]
  [--json]
  [--pretty]
  [--dir PATH]
```

Task arguments resolve by ID first and exact name second. Omitting task
arguments queries by the supplied filters. At least one task argument or filter
is required.

Defaults:

- `limit`: 25
- maximum `limit`: 100
- fields: `id`, `name`, `status`, `parent_id`, `depends_on`, `assignee`,
  `claimed_by`, `updated_at`, `description_preview`, `description_bytes`,
  `output_preview`, and `output_bytes`
- preview length: at most 240 Unicode code points

Full `description`, `output`, `path`, `claim_path`, and complete timestamps are
returned only when named in `--fields`.

### New MCP tool

Expose the same operation as `plan_get_tasks`:

```json
{
  "plan": "c23684f0",
  "tasks": ["347-repair-shared-plan-record-integrity"],
  "status": ["complete"],
  "parent": "286-phase-4-run-cost-approved-live-qualification",
  "claimed_by": "codex-root",
  "fields": ["id", "name", "status", "description"],
  "limit": 25,
  "cursor": null,
  "known_version": 1708,
  "root": "/workspace"
}
```

All filters are optional, but the request must provide at least one task
reference or filter.

The result shape is:

```json
{
  "plan_id": "c23684f0",
  "plan_name": "Expand Redis Skill Eval Coverage to Every Skill",
  "version": 1709,
  "unchanged": false,
  "returned": 1,
  "next_cursor": null,
  "fields": ["id", "name", "status", "description"],
  "tasks": [
    {
      "id": "347-repair-shared-plan-record-integrity",
      "name": "Repair shared plan record integrity",
      "status": "complete",
      "description": "..."
    }
  ]
}
```

### Pagination

Cursors are opaque. They bind:

- plan ID and version
- normalized filters
- requested fields
- last returned task ordering key

Using a cursor after the plan version changes returns `cursor_stale`. The
caller must restart the query. The server must not silently mix pages from
different plan versions.

### Unchanged responses

`plan_stats`, `plan_ready`, `plan_get_tasks`, `plan_get`, and `plan_show` accept
an optional `known_version`.

When the current plan version matches, they return:

```json
{"plan_id":"c23684f0","version":1708,"unchanged":true}
```

No task records are loaded for an unchanged response.

### Full-plan operations

`get` and `show` keep their current full-plan semantics. They must include a
tool description warning that the result can be large and that agents should
prefer `plan_stats`, `plan_ready`, or `plan_get_tasks`.

The current Mermaid output remains an explicit compact topology view and is not
changed by this specification.

## Output Contract

### Agent-plan MCP

For a successful MCP call:

- `structuredContent` contains the canonical machine-readable result.
- `content` contains one text item of at most 512 UTF-8 bytes.
- The text summarizes the operation and key counts.
- The text does not contain serialized JSON, YAML, task descriptions, or task
  outputs.
- The same collection or object does not appear twice in
  `structuredContent`.

Examples:

```text
Plan c23684f0: 348 tasks, 3 ready, version 1708.
Loaded 2 tasks from plan c23684f0 at version 1708.
Updated task 348-run-approved-remediated-redis-insight-qualification.
```

`toolOK` must stop marshaling the structured value into text.

### Agent-plan CLI

`--json` emits compact JSON followed by one newline. `--pretty` requires
`--json` and enables two-space indentation.

Human-readable output does not change. Full descriptions and outputs remain
hidden unless the command's existing verbose behavior or a new `--fields`
request asks for them.

### Dashboard MCP

Dashboard MCP responses use one envelope:

```json
{
  "ok": true,
  "operation": "dashboard_get_self_context",
  "target": {"scope": "self"},
  "data": {},
  "warnings": []
}
```

Payload fields appear only under `data`; they are not copied to the top level.
As with agent-plan, `content[].text` is a short summary and does not serialize
`structuredContent`.

`dashboard_get_self_context` accepts:

```json
{
  "detail": "compact",
  "include": []
}
```

`detail` defaults to `compact`. The compact response contains:

- availability and resolution source
- session ID, provider, workspace root, workspace name, status, task-list ID,
  and current-task ID
- bound plan ID and name
- queue pause state and count
- loop enabled and paused state
- runtime status
- peer count

The `include` array may request `provider_features`, `runtime`, `queue_messages`,
`loop_configuration`, `peers`, or `relationships`. `detail: "full"` is an
explicit request for all optional sections.

## Size Budgets

These budgets apply to default structured content before transport framing:

| Operation | Budget |
|---|---:|
| `plan_stats` | 2 KiB |
| `plan_audit` with no findings | 2 KiB |
| `plan_ready` first page | 32 KiB |
| `plan_get_tasks` first page | 64 KiB |
| compact dashboard self-context | 4 KiB |
| MCP text content | 512 bytes |

If requested fields exceed a bounded operation's budget, the tool returns fewer
items with `next_cursor`. A single explicitly requested field that exceeds the
budget may be returned in full, because the caller opted into it. The response
must report its byte count.

`plan_get` and `plan_show` are exempt because their names explicitly request a
full snapshot. They still follow the no-duplication text contract.

## Agent Guidance

The source agent-plan skill must adopt this cold-start sequence:

1. `stats` to identify version, counts, and health.
2. `audit` only when health is not `healthy` or coordination looks suspicious.
3. `ready` or `next` to find actionable work.
4. `get-task` or `plan_get_tasks` for the selected task and its direct
   dependencies.
5. `show` or `get` only when the agent truly needs the complete graph.

Mutation guidance must also require:

- an explicit canonical `--dir` when the target workspace is known
- one mutation per tool call
- no mutation piped into `jq` or another process
- a separate `stats`, `audit`, or targeted read after material mutations

Skill changes must be made in the source skill through the supported draft and
promotion workflow, not by editing rendered agent directories.

## Compatibility and Migration

### Agent-plan

- Manifest version 1 remains readable.
- The first successful mutation upgrades that plan to version 2.
- `get`, `show`, and existing mutation schemas retain their semantic fields.
- CLI compact JSON changes whitespace only.
- MCP text changes from duplicated JSON to a summary. Consumers must read
  `structuredContent`, which is already the canonical structured channel.
- `plan_get_tasks`, pagination, field selection, and `known_version` are
  additive.

### Dashboard

The dashboard envelope change removes compatibility aliases. Roll it out in two
steps:

1. Add the single-envelope implementation and update dashboard consumers,
   tests, skills, and examples to read `structuredContent.data`.
2. Remove top-level aliases after one release. During the transition, callers
   may request `response_version: 1`; the default remains the compact,
   single-envelope response.

The compatibility mode must never be used in skill examples or agent guidance.

## Implementation Map

| Area | Primary code |
|---|---|
| Root identity, record loading, persistence, and migration | `store.go` |
| Stable mutation IDs and task ordering | `store.go`, `model.go`, `bulk.go` |
| Manifest and bounded task-view types | `types.go` |
| `get-task`, compact JSON, and typed CLI errors | `cli.go` |
| MCP schemas, `plan_get_tasks`, known versions, and text summaries | `mcp.go` |
| MCP wire result contract | `internal/mcpproto/` |
| Store and path-alias regressions | `store_test.go` |
| CLI and output-budget regressions | `cli_test.go` |
| MCP envelope and retrieval regressions | `mcp_test.go` |
| Dashboard envelope and compact self-context | `agent-dashboard/src/agent_dashboard/mcp_surface.py` and `dashboard_service_impl.py` |
| Source agent guidance | upstream agent-plan skill source, changed through the supported draft workflow |

## Observability

Both tools should record:

- structured-content bytes
- text-content bytes
- returned item count
- omitted item count
- whether pagination or an unchanged response was used
- requested fields
- plan version
- operation duration

Do not record full task descriptions, outputs, or dashboard queue messages in
these metrics.

Add a warning when:

- an MCP result exceeds its default budget
- a full-plan tool returns more than 128 KiB
- a caller requests `detail: "full"`
- a version 1 manifest is loaded
- cleanup leaves an orphan record

## Testing

### Store safety

1. Initialize a plan through symbolic-link alias A.
2. Create at least 350 mixed-status tasks with hierarchy, dependencies,
   descriptions, outputs, and claims.
3. Open and mutate it through the real path and alias B.
4. Complete one task and update another.
5. Assert that every unaffected record still exists with the same checksum.
6. Assert that the completed task exists only in history.
7. Assert that `show`, `stats`, `ready`, and `audit` succeed.
8. Assert that the manifest contains only relative paths.

Add fault-injection tests before and after each write, manifest commit, and
cleanup step. After every injected failure, either the old or new manifest must
load with every referenced record present.

### Mutation identity

Create tasks whose requested order forces in-place sorting. For each mutation,
assert that:

- the returned ID is the created or updated ID
- the returned record matches the manifest entry
- the returned record matches the durable task file

Run the test across randomized orderings and at least 1,000 tasks.

### Errors and degraded audit

- Remove one record referenced by a manifest.
- Assert that `show` returns `task_record_missing`, not `plan_not_found`.
- Assert that `audit` succeeds and reports the missing task and path.
- Assert that `stats` returns `health: "degraded"`.
- Add an orphan task and claim file and assert that `audit` reports both.

### Output and retrieval

- Assert that MCP text never contains serialized structured content.
- Assert that each semantic collection appears once in structured content.
- Verify every default size budget.
- Verify that descriptions and outputs are absent unless explicitly requested.
- Verify field selection, exact task resolution, filters, pagination, stale
  cursors, and unchanged responses.
- Compare the incident workflow before and after the change. The compact
  workflow must reduce returned bytes by at least 90 percent while preserving
  every field used for plan selection and session binding.

### Compatibility

- Load a version 1 manifest with absolute paths and read it without mutation.
- Mutate it through a different symbolic-link route and verify safe conversion
  to version 2.
- Run existing CLI and MCP tests against compact and pretty JSON.
- Run dashboard contract tests against response versions 1 and 2 during the
  transition.

## Acceptance Criteria

The work is complete when:

- No supported path alias can cause a durable record to be removed.
- A failed mutation leaves a readable old or new plan with no missing
  referenced records.
- The AddTask regression always returns the newly created task.
- Plan corruption produces a specific, evidence-carrying error.
- `audit` can diagnose missing records without loading them.
- Routine agent startup uses bounded tools rather than a full plan snapshot.
- No MCP success response serializes its structured payload into text.
- Dashboard context contains one payload copy and defaults to compact detail.
- Default reads meet the documented byte budgets.
- The incident-scale end-to-end regression shows at least a 90 percent byte
  reduction.
- Agent-plan, dashboard, and source-skill tests pass.

## Rollout Order

1. Add failing store, mutation-identity, and error-propagation regressions.
2. Implement canonical roots, version 2 manifests, and safe commit order.
3. Implement degraded-store `audit` and `stats`.
4. Add `get-task`, `plan_get_tasks`, pagination, fields, and known versions.
5. Change agent-plan MCP text and CLI JSON formatting.
6. Change the dashboard envelope and compact self-context.
7. Update source skills and examples.
8. Run the incident-scale end-to-end regression and record byte measurements.
