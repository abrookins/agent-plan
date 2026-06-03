# Agent-Plan Manual QA Spec

This document defines the manual QA plan for the Go `agent-plan` binary. It is
intended for release validation, regression testing, and focused verification of
feature work that changes the CLI, MCP server, filesystem store, migration
behavior, or claim-aware task coordination.

## Scope

This spec covers every user-facing feature exposed by the Go binary:

- CLI commands
- compatibility aliases
- JSON output paths
- MCP stdio server behavior
- filesystem layout and migration
- claim-aware task selection
- exclusive-claim coordination
- guidance installation targets

This spec does not cover the importable Go library API directly. Library
behavior is validated indirectly through the binary surfaces that exercise it.

## Release Matrix

Run the full manual suite on at least one primary development platform and one
release target. Recommended matrix:

| Environment | Purpose |
| --- | --- |
| macOS arm64 | primary developer workflow |
| Linux amd64 | common CI and server workflow |
| Linux arm64 | ARM release validation |
| Windows amd64 | binary startup, path handling, and basic CLI smoke checks |

For every tagged release, at minimum:

- run the full suite on one Unix-like platform
- run smoke coverage on every published target architecture
- rerun migration, claim coordination, MCP, and install tests on the release
  binary rather than `go run`

## Prerequisites

- repository checkout at `~/src/agent-plan`
- Go toolchain installed
- `python3` available for MCP transport checks
- `jq` available for JSON inspection
- a clean temporary workspace outside the repo for destructive test cases
- for `install --global` cases, prefer a temporary `HOME` override so the test
  does not modify a real user profile

Build the binary under test:

```bash
cd ~/src/agent-plan
make build
```

Set a reusable workspace:

```bash
export AP_BIN="$PWD/dist/agent-plan-$(go env GOOS)-$(go env GOARCH)"
export AP_WORKDIR="$(mktemp -d)"
mkdir -p "$AP_WORKDIR"
cd "$AP_WORKDIR"
```

On Windows, append `.exe` to `AP_BIN`.

Unless a case says otherwise, run all commands inside the temporary workspace.
Prepend `"$AP_BIN"` to every command example in the cases below.

## Global Acceptance Criteria

- every successful command exits `0`
- every failing validation path exits non-zero
- human-readable output is coherent and stable enough for operators
- `--json` output is valid JSON and contains the expected fields
- `.agent-plan/` files match the documented layout
- no command writes outside the workspace unless the test explicitly uses
  `install --global`

## Test Data Conventions

Use these identifiers unless a case needs different values:

- plan names: `Alpha Plan`, `Beta Plan`
- agent IDs: `agent-a`, `agent-b`
- task names: `Task A`, `Task B`, `Task C`
- spec path: `docs/spec.md`

Create a placeholder spec file when needed:

```bash
mkdir -p docs
printf '# Spec\n' > docs/spec.md
```

## CLI QA Cases

### QA-CLI-01: `init` creates the workspace store

Steps:

1. Run `"$AP_BIN" init --json`.
2. Inspect `.agent-plan/`.

Expected results:

- JSON contains `root` and `meta_dir`
- `.agent-plan/workspace.json` exists
- `.agent-plan/plans/` exists

### QA-CLI-02: `create`, `list`, `get`, `update`, and `delete` manage plans

Steps:

1. Run `create` twice for `Alpha Plan` and `Beta Plan`.
2. Run `list --json`.
3. Run `get <plan-id> --json` for `Alpha Plan`.
4. Run `update <plan-id> --description "Updated" --spec-path docs/spec.md --json`.
5. Run `delete <plan-id> --yes --json` on `Beta Plan`.

Expected results:

- both plans appear in `list`
- `get` returns stable plan metadata plus task summary fields
- `update` persists `description` and `spec_path`
- deleted plans disappear from subsequent `list` and `get` fails for that plan

### QA-CLI-03: `delete` refuses destructive action without confirmation

Steps:

1. Create a throwaway plan.
2. Run `delete <plan-id>` without `--yes`.
3. Answer `n` at the prompt.

Expected results:

- command exits non-zero or reports cancellation
- the plan still exists

### QA-CLI-04: `add-task` creates ordered tasks with metadata

Steps:

1. Create a plan if needed.
2. Run:
   `add-task <plan-id> "Task A" --description "first" --json`
3. Run:
   `add-task <plan-id> "Task B" --description "second" --depends-on <task-a-id> --json`
4. Run:
   `add-task <plan-id> "Task C" --parent <task-a-id> --order 30 --json`
5. Inspect `.agent-plan/plans/<plan-id>/tasks/`.

Expected results:

- one Markdown task file exists per task
- frontmatter contains task metadata
- manifest ordering and dependency fields reflect the command inputs

### QA-CLI-05: `update-task` mutates status and metadata

Steps:

1. Run `update-task` on `Task A` with:
   `--status in_progress --description "updated" --claimed-by agent-a --json`
2. Run `update-task` again with:
   `--depends-on <task-b-id> --order 50 --json`
3. Run `update-task` again with:
   `--clear-dependencies --clear-claimed-by --status blocked --json`

Expected results:

- status transitions persist
- updated description persists
- `claimed_by` is added and then removed
- dependencies and order update correctly

### QA-CLI-06: `delete-task` removes active task records

Steps:

1. Delete `Task C`.
2. Run `get <plan-id> --json`.
3. Inspect the task file path directly.

Expected results:

- deleted task no longer appears in plan output
- the task Markdown file is removed
- other tasks remain intact and ordered

### QA-CLI-07: `ready` filters by dependency readiness rather than hierarchy

Steps:

1. Create a parent task with no dependencies.
2. Create a child task using `--parent` only.
3. Create another task with `--depends-on <parent-id>`.
4. Run `ready <plan-id> --json`.

Expected results:

- parent task is ready
- child task without dependencies is also ready
- dependency-blocked task is absent until its dependency completes

### QA-CLI-08: `next` prefers already-claimed work for a claimer

Steps:

1. Claim `Task A` for `agent-a`.
2. Leave `Task B` unclaimed and ready.
3. Run `next <plan-id> --by agent-a --json`.
4. Run `next <plan-id> --by agent-b --json`.

Expected results:

- `agent-a` receives `Task A`
- `agent-b` receives the best unclaimed ready task

### QA-CLI-09: `claim-next` creates exclusive claim state and assigns the task

Steps:

1. Run `claim-next <plan-id> --by agent-a --json`.
2. Inspect `.agent-plan/plans/<plan-id>/tasks/<task-id>.claim`.
3. Run `get <plan-id> --json`.

Expected results:

- the returned task is ready and now claimed
- a `.claim` file exists
- the task shows `claimed_by: agent-a`

### QA-CLI-10: `show` returns the full current plan state

Steps:

1. Run `show <plan-id> --json`.
2. Run `show <plan-id>` without `--json`.

Expected results:

- the JSON payload includes the full plan object and all tasks
- the text output includes the plan metadata plus task state
- `show` succeeds with the same plan references accepted by `get`

### QA-CLI-11: removed implicit-plan commands fail explicitly

Steps:

1. Run:
   `add "Alias Task" --description "alias path" --json`
2. Run:
   `list-tasks --json`
3. Run:
   `claim <task-id> --json`
4. Run:
   `block <task-id> --reason "manual block" --json`
5. Run:
   `done <task-id> --output "done output" --json`
6. Run:
   `plan show --json`

Expected results:

- each command exits non-zero
- the error makes it clear the command is unsupported or unknown

### QA-CLI-12: `install` writes workspace-local guidance

Steps:

1. Run `install AGENTS.md --json`.
2. Run `install CLAUDE.md --json`.
3. Run `install skill --json`.
4. Re-run all three commands a second time.

Expected results:

- workspace files are written to:
  - `./AGENTS.md`
  - `./CLAUDE.md`
  - `./.codex/skills/agent-plan-management/SKILL.md`
- managed guidance is idempotent
- re-running updates the managed block rather than duplicating it

### QA-CLI-13: `install --global` writes to provider home

Steps:

1. Run `install --global AGENTS.md --json`.
2. Run `install --global CLAUDE.md --json`.
3. Run `install --global skill --json`.

Expected results:

- files are written under:
  - `$HOME/.codex/AGENTS.md`
  - `$HOME/.claude/CLAUDE.md`
  - `$HOME/.codex/skills/agent-plan-management/SKILL.md`
- no workspace-local guidance file is required for the global case

### QA-CLI-14: usage and validation failures are explicit

Verify these failure cases:

- unknown command
- missing required plan ID
- invalid status for `update-task`
- unsupported `install` target

Expected results:

- commands exit non-zero
- usage text or error text clearly identifies what is missing or invalid

### QA-CLI-15: environment fallbacks stay explicit

Steps:

1. Export `AGENT_PLAN_DIR=<workspace-root>`.
2. Run `list --json` without `--dir`.
3. Export `AGENT_PLAN_NONINTERACTIVE=1`.
4. Run `delete <plan-id> --json` without `--yes`.

Expected results:

- `list` succeeds against the workspace from `AGENT_PLAN_DIR`
- `delete` succeeds without prompting when `AGENT_PLAN_NONINTERACTIVE=1`
- explicit `--dir` remains preferred when the caller can provide it

## JSON Output QA

### QA-JSON-01: agent-facing commands emit valid JSON

Run `--json` for:

- `init`
- `create`
- `list`
- `get`
- `update`
- `delete`
- `add-task`
- `update-task`
- `delete-task`
- `ready`
- `next`
- `claim-next`
- `install`

Expected results:

- `jq .` succeeds on every response
- top-level payload shape matches the command semantics
- IDs, status fields, and file paths are present when relevant

### QA-JSON-02: `mcp` does not accept CLI JSON mode

Steps:

1. Run `"$AP_BIN" mcp --json`.

Expected results:

- the command rejects the flag or reports usage
- the binary does not mix CLI JSON mode into the MCP transport

## MCP QA Cases

### QA-MCP-01: server starts and responds to `initialize`

Use a simple stdio client. Example:

```bash
python3 - <<'PY'
import json, os, subprocess

proc = subprocess.Popen(
    [os.environ["AP_BIN"], "mcp"],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    text=True,
)

messages = [
    {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"qa","version":"1"}}},
    {"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}},
]

for msg in messages:
    proc.stdin.write(json.dumps(msg) + "\n")
proc.stdin.flush()

for _ in range(2):
    print(proc.stdout.readline().strip())

proc.kill()
PY
```

Expected results:

- `initialize` returns server metadata
- `tools/list` includes the documented plan tools

Adjust the binary path if needed for the test environment.

### QA-MCP-02: primary MCP tools mirror CLI behavior

Exercise each tool with valid inputs:

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
- `plan_ready`
- `plan_next`
- `plan_claim_next`
- `plan_install`

Expected results:

- every tool succeeds with structured content and text output
- payloads reflect the same state transitions as the CLI
- plan and task identifiers remain consistent across calls

### QA-MCP-03: removed implicit-plan MCP tools fail explicitly

Exercise:

- `plan_add`
- `plan_claim`
- `plan_done`
- `plan_block`

Expected results:

- each tool returns an MCP error response

### QA-MCP-04: MCP validation failures are explicit

Verify failure paths for:

- missing required arguments
- invalid task ID
- invalid plan ID
- unsupported install target

Expected results:

- MCP error responses are returned
- failures do not corrupt workspace state

## Filesystem QA

### QA-FS-01: active layout matches the documented structure

After exercising plan and task creation, inspect:

- `.agent-plan/workspace.json`
- `.agent-plan/plans/<plan-id>/plan.json`
- `.agent-plan/plans/<plan-id>/manifest.json`
- `.agent-plan/plans/<plan-id>/tasks/*.md`

Expected results:

- files exist where documented
- contents are internally consistent
- IDs referenced in the manifest correspond to task files

### QA-FS-02: completing work archives history

Steps:

1. Complete a task through `done`.
2. Inspect `history/`.

Expected results:

- the completed record is preserved in `history/*.done`
- active task state no longer lists the completed item where appropriate

### QA-MIG-01: legacy single-plan workspace is rejected

Steps:

1. Create a synthetic legacy workspace containing:
   - `.agent-plan/manifest.json`
   - `.agent-plan/tasks/`
   - `.agent-plan/history/`
2. Run `list` or `create`.

Expected results:

- the command fails clearly because the workspace is not initialized in the
  current format
- no automatic migration occurs

### QA-MIG-02: explicit commands continue to work after fresh initialization

Steps:

1. Initialize a fresh workspace.
2. Create a plan and add tasks.
3. Run `get <plan-id> --json`.

Expected results:

- the explicit plan commands report the state consistently

## Concurrency and Coordination QA

### QA-CLAIM-01: only one claimant wins an unclaimed task

Steps:

1. Create a plan with exactly one ready task.
2. In two terminals, run `claim-next <plan-id> --by agent-a --json` and
   `claim-next <plan-id> --by agent-b --json` as close together as
   possible.

Expected results:

- exactly one command succeeds in claiming the task
- the other command reports no claimable task or a claim collision
- the final task state shows exactly one assignee and one `.claim` file

### QA-CLAIM-02: `next` and `claim-next` honor claimer preference

Steps:

1. Create two ready tasks.
2. Claim one for `agent-a`.
3. Run `next` and `claim-next` with `--by agent-a` and `--by agent-b`.

Expected results:

- `agent-a` prefers its claimed task
- unclaimed work is still available to other claimers

## Cross-Workspace QA

### QA-DIR-01: `--dir` isolates workspace state

Steps:

1. Create two separate temporary workspaces.
2. Run the same commands with `--dir` against each.

Expected results:

- state does not leak across workspaces
- explicit plan operations stay scoped to the selected workspace

## Cleanup

After the manual run:

```bash
rm -rf "$AP_WORKDIR"
```

If global install cases were run, remove only the managed blocks or files that
the test created.

## Signoff Checklist

Do not sign off on a release until all of the following are true:

- all CLI primary commands passed
- all documented JSON output paths were validated
- all MCP tools passed initialize, discovery, and execution checks
- legacy-workspace rejection behavior passed
- exclusive-claim concurrency passed
- workspace-local and global install flows passed
- at least one Unix-like release binary passed the full suite
- all additional release targets passed startup and smoke verification
