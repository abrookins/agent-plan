# agent-plan

`agent-plan` is a filesystem-backed replacement for the Python `agent-plan` CLI.
It stores plan state in a workspace-local `.agent-plan/` directory so the same
plan can work on local disk, an AFS mount, or any other filesystem that
preserves `O_CREATE|O_EXCL` semantics for exclusive file creation.

## Install

From the repo root:

```bash
go install ./cmd/agent-plan
```

That installs the binary into `$(go env GOBIN)` when `GOBIN` is set, or
`$(go env GOPATH)/bin` otherwise. Make sure that directory is on your `PATH`,
then you can run:

```bash
agent-plan help
```

## Build

To build a local binary from the repo root:

```bash
go build -o agent-plan ./cmd/agent-plan
```

A plain `go build` at the repo root does not leave an executable behind because
the root package is the importable `agentplan` library, while the binary entry
point lives under `./cmd/agent-plan`.

## Layout

```text
.agent-plan/
  manifest.json
  manifest.lock
  tasks/
    001-fix-auth.md
    001-fix-auth.claim
  history/
    001-fix-auth.done
```

- `manifest.json` is the query index for dashboards and fast task listing.
- `tasks/*.md` is the source of truth for active tasks.
- `tasks/*.claim` is the task-claim coordination primitive.
- `history/*.done` preserves completed task records for auditability.

## Git

Do not commit `.agent-plan/` by default. It is often local working state for
one agent or one machine.

If a team wants repo-shared planning, commit the durable records:

- `manifest.json`
- `tasks/*.md`
- `history/*.done` when you want audit history in git

Do not commit the coordination files:

- `manifest.lock`
- `tasks/*.claim`

Example `.gitignore` entries:

```gitignore
.agent-plan/manifest.lock
.agent-plan/tasks/*.claim
```

## CLI

```bash
go run ./cmd/agent-plan init
go run ./cmd/agent-plan add "fix the auth bug" --description "Reproduce and patch"
go run ./cmd/agent-plan list
go run ./cmd/agent-plan next
go run ./cmd/agent-plan claim 001-fix-auth
go run ./cmd/agent-plan done 001-fix-auth --output "PR #47"
go run ./cmd/agent-plan block 001-fix-auth --reason "Waiting on API contract"
go run ./cmd/agent-plan plan show
go run ./cmd/agent-plan mcp
```

All commands accept `--dir <workspace-root>`. When omitted, `agent-plan`
searches upward from the current working directory for `.agent-plan/`.

## MCP

The stdio server exposes:

- `plan_init`
- `plan_add`
- `plan_list`
- `plan_next`
- `plan_claim`
- `plan_done`
- `plan_block`
- `plan_show`

Example client config:

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

## AFS note

On a mounted filesystem, claim files use `O_CREATE|O_EXCL` directly. If an agent
needs to claim through an MCP interface instead of a mount, the filesystem MCP
server needs an exclusive-create file tool so the same coordination primitive
can be preserved remotely.
