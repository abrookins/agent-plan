package agentplan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	codexGuidanceStart      = "<!-- agent-plan:codex-guidance:start -->"
	codexGuidanceEnd        = "<!-- agent-plan:codex-guidance:end -->"
	claudeGuidanceStart     = "<!-- agent-plan:claude-guidance:start -->"
	claudeGuidanceEnd       = "<!-- agent-plan:claude-guidance:end -->"
	planManagementSkillSlug = "agent-plan-management"
)

func InstallGuidance(target string, globalInstall bool, workspaceRoot string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(target))
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		cwd, err := getwdFunc()
		if err != nil {
			return "", err
		}
		root = cwd
	}

	switch normalized {
	case "agents.md":
		destination := filepath.Join(root, "AGENTS.md")
		if globalInstall {
			destination = filepath.Join(os.Getenv("HOME"), ".codex", "AGENTS.md")
		}
		if err := upsertManagedGuidance(destination, buildCodexGuidance(), codexGuidanceStart, codexGuidanceEnd); err != nil {
			return "", err
		}
		return destination, nil
	case "claude.md":
		destination := filepath.Join(root, "CLAUDE.md")
		if globalInstall {
			destination = filepath.Join(os.Getenv("HOME"), ".claude", "CLAUDE.md")
		}
		if err := upsertManagedGuidance(destination, buildClaudeGuidance(), claudeGuidanceStart, claudeGuidanceEnd); err != nil {
			return "", err
		}
		return destination, nil
	case "skill":
		skillRoot := filepath.Join(root, ".codex", "skills")
		if globalInstall {
			skillRoot = filepath.Join(os.Getenv("HOME"), ".codex", "skills")
		}
		destination := filepath.Join(skillRoot, planManagementSkillSlug, "SKILL.md")
		if err := mkdirAllFunc(filepath.Dir(destination), defaultDirPerm); err != nil {
			return "", err
		}
		if err := atomicWriteFile(destination, []byte(buildPlanManagementSkill()+"\n"), defaultFilePerm); err != nil {
			return "", err
		}
		return destination, nil
	default:
		return "", fmt.Errorf("unsupported install target: %s", target)
	}
}

func upsertManagedGuidance(path, guidance, startMarker, endMarker string) error {
	existing := ""
	if data, err := readFileFunc(path); err == nil {
		existing = string(data)
	} else if !errorsIsNotExist(err) {
		return err
	}
	managedBlock := startMarker + "\n" + guidance + "\n" + endMarker + "\n"
	pattern := regexp.MustCompile(regexp.QuoteMeta(startMarker) + `(?s:.*?)` + regexp.QuoteMeta(endMarker) + `\n?`)
	updated := ""
	if pattern.MatchString(existing) {
		updated = pattern.ReplaceAllString(existing, managedBlock)
	} else {
		prefix := strings.TrimRight(existing, "\n")
		if prefix != "" {
			updated = prefix + "\n\n" + managedBlock
		} else {
			updated = managedBlock
		}
	}
	return atomicWriteFile(path, []byte(updated), defaultFilePerm)
}

func buildCodexGuidance() string {
	return strings.TrimSpace(`
## Agent Plan Management

When the prompt or current work references a shared plan, task list, or explicit plan id, coordinate through ` + "`agent-plan`" + `.

- Start with ` + "`agent-plan show <plan_id> --json`" + ` or ` + "`agent-plan get <plan_id> --json`" + `
- Use ` + "`--dir <workspace-root>`" + ` when operating outside the target workspace; otherwise ` + "`AGENT_PLAN_DIR`" + ` and then the current directory are used
- Mark tasks ` + "`in_progress`" + ` when you start: ` + "`agent-plan update-task <plan_id> <task_id> --status in_progress --json`" + `
- Mark tasks ` + "`complete`" + ` when you finish: ` + "`agent-plan update-task <plan_id> <task_id> --status complete --json`" + `; completed records move into ` + "`history/*.done`" + `
- Add discovered work with ` + "`agent-plan add-task <plan_id> \"Task name\" --description \"Why it is needed\" --json`" + `
- Use ` + "`--depends-on <task_id>`" + ` for blocking relationships; use ` + "`--parent <task_id>`" + ` only for hierarchy or subtasks
- ` + "`agent-plan ready`" + `, ` + "`agent-plan next`" + `, and ` + "`agent-plan claim-next`" + ` only use dependencies for readiness; parent-child nesting alone does not block selection
- Use ` + "`agent-plan claim-next <plan_id> --by <agent-id> --json`" + ` when you need exclusive ownership before file edits
- Use ` + "`agent-plan next <plan_id> --by <agent-id> --json`" + ` when claimer affinity matters; plain ` + "`next`" + ` does not infer external claimer context
- Use ` + "`agent-plan install AGENTS.md|CLAUDE.md|skill`" + ` only for onboarding or guidance setup, not normal plan lifecycle updates
- Prefer task and plan IDs. Names must match exactly when used as references
`)
}

func buildClaudeGuidance() string {
	return buildCodexGuidance()
}

func buildPlanManagementSkill() string {
	return strings.TrimSpace(`
---
name: agent-plan-management
description: Use when a prompt references a shared plan, task list, or explicit plan_id and the agent should coordinate work through the agent-plan CLI.
---

# agent-plan-management

Use this skill when a prompt references a shared plan, task list, or explicit ` + "`plan_id`" + `.

## Required behavior

- Start with ` + "`agent-plan show <plan_id> --json`" + ` or ` + "`agent-plan get <plan_id> --json`" + ` to load the whole plan and task state.
- Add ` + "`--dir <workspace-root>`" + ` when you are outside the target workspace. Without it, the CLI uses ` + "`AGENT_PLAN_DIR`" + ` and then the current directory.
- Claim ready work before editing when coordination matters: ` + "`agent-plan claim-next <plan_id> --by <agent-id> --json`" + `.
- Mark the current task ` + "`in_progress`" + ` when you begin work: ` + "`agent-plan update-task <plan_id> <task_id> --status in_progress --json`" + `.
- Mark the task ` + "`complete`" + ` when you finish: ` + "`agent-plan update-task <plan_id> <task_id> --status complete --json`" + `. Completed records move into ` + "`history/*.done`" + `.
- Add discovered follow-up work with ` + "`agent-plan add-task <plan_id> \"Task name\" --description \"Why it is needed\" --json`" + `.
- Use ` + "`--depends-on <task_id>`" + ` for blocking relationships.
- Use ` + "`--parent <task_id>`" + ` only for hierarchy or subtasks.
- ` + "`agent-plan ready`" + `, ` + "`agent-plan next`" + `, and ` + "`agent-plan claim-next`" + ` only use ` + "`depends_on`" + ` for readiness; a child task without dependencies is still ready.
- Use ` + "`agent-plan next <plan_id> --by <agent-id> --json`" + ` when claimer affinity matters; plain ` + "`next`" + ` does not infer external claimer context.
- Prefer task and plan IDs. Names must match exactly when used as references.

## Current CLI Surface

Plan lifecycle:

` + "```bash" + `
agent-plan init --json
agent-plan create "Plan name" --description "Why this exists" --json
agent-plan list --json
agent-plan show <plan_id> --json
agent-plan update <plan_id> --name "New name" --json
agent-plan delete <plan_id> --yes --json
` + "```" + `

Task lifecycle:

` + "```bash" + `
agent-plan add-task <plan_id> "Task name" --description "Why it is needed" --json
agent-plan update-task <plan_id> <task_id> --status in_progress --json
agent-plan update-task <plan_id> <task_id> --status blocked --json
agent-plan update-task <plan_id> <task_id> --status complete --json
agent-plan delete-task <plan_id> <task_id> --json
` + "```" + `

Coordination:

` + "```bash" + `
agent-plan ready <plan_id> --json
agent-plan next <plan_id> --by <agent-id> --json
agent-plan claim-next <plan_id> --by <agent-id> --json
` + "```" + `

Other current commands:

- ` + "`agent-plan install [--global] AGENTS.md|CLAUDE.md|skill`" + ` installs managed guidance.
- ` + "`agent-plan mcp`" + ` runs the stdio MCP server; it does not use ` + "`--json`" + `.
- ` + "`agent-plan version --json`" + ` prints the CLI version.
- ` + "`agent-plan help <command>`" + ` and ` + "`agent-plan <command> --help`" + ` show command-specific flags.

## Task State Rules

- Valid statuses are ` + "`not_started`" + `, ` + "`in_progress`" + `, ` + "`blocked`" + `, ` + "`complete`" + `, and ` + "`cancelled`" + `.
- ` + "`update-task --status complete`" + ` archives the active task record into ` + "`history/*.done`" + `.
- ` + "`update-task`" + ` can also change ` + "`--name`" + `, ` + "`--description`" + `, ` + "`--claimed-by`" + `, ` + "`--clear-claimed-by`" + `, ` + "`--depends-on`" + `, ` + "`--clear-dependencies`" + `, and ` + "`--order`" + `.
- ` + "`delete`" + ` prompts unless ` + "`--yes`" + ` is passed or ` + "`AGENT_PLAN_NONINTERACTIVE=1`" + ` is set.
- Agent-facing commands support ` + "`--json`" + `; ` + "`mcp`" + ` uses the MCP wire format instead.

## MCP Surface

Prefer MCP tools when the host agent already wants tool calls instead of shell commands.

Available plan tools:

- ` + "`plan_init`" + `
- ` + "`plan_create`" + `
- ` + "`plan_list`" + `
- ` + "`plan_get`" + `
- ` + "`plan_show`" + `
- ` + "`plan_update`" + `
- ` + "`plan_delete`" + `
- ` + "`plan_add_task`" + `
- ` + "`plan_update_task`" + `
- ` + "`plan_delete_task`" + `
- ` + "`plan_ready`" + `
- ` + "`plan_next`" + `
- ` + "`plan_claim_next`" + `
- ` + "`plan_install`" + `

## Example

` + "```bash" + `
agent-plan add-task <plan_id> "UI follow-up" \
  --parent <epic_task_id> \
  --depends-on <api_task_id>
` + "```" + `

## Durable Files

- Treat ` + "`tasks/*.md`" + ` and ` + "`history/*.done`" + ` as durable records.
- Treat ` + "`tasks/*.claim`" + ` and ` + "`manifest.lock`" + ` as coordination state.
- Prefer the CLI or MCP tools over editing ` + "`.agent-plan/`" + ` files by hand.
`)
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
