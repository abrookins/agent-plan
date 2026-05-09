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

When the prompt or current work references a shared plan or task list, use ` + "`agent-plan`" + `.

- Start with ` + "`agent-plan get <plan_id>`" + `
- If you create or choose a plan while already running, bind it with ` + "`agent-plan bind-current <plan_name_or_id>`" + `
- Mark tasks ` + "`in_progress`" + ` when you start
- Mark tasks ` + "`complete`" + ` when you finish
- Add discovered work with ` + "`agent-plan add-task <plan_id> \"Task name\" --description \"Why it is needed\"`" + `
- Use ` + "`--depends-on <task_id>`" + ` when a task is blocked by another task
- Use ` + "`--parent <task_id>`" + ` only for hierarchy; parent-child nesting does not block ` + "`ready`" + ` or ` + "`next`" + `
- Prefer task IDs, but use unique plan and task names when supported
`)
}

func buildClaudeGuidance() string {
	return buildCodexGuidance()
}

func buildPlanManagementSkill() string {
	return strings.TrimSpace(`
# agent-plan-management

Use this skill when a prompt references a shared plan, task list, or explicit ` + "`plan_id`" + `.

## Required behavior

- Start with ` + "`agent-plan get <plan_id>`" + `
- Mark the current task ` + "`in_progress`" + ` when you begin work
- Mark the task ` + "`complete`" + ` when you finish
- Add discovered follow-up work with ` + "`agent-plan add-task <plan_id> \"Task name\" --description \"Why it is needed\"`" + `
- Use ` + "`--depends-on <task_id>`" + ` for blocking relationships
- Use ` + "`--parent <task_id>`" + ` only for hierarchy or subtasks
- ` + "`agent-plan ready`" + ` and ` + "`agent-plan next`" + ` only use ` + "`depends_on`" + `; a child task without dependencies is still ready

## Example

` + "```bash" + `
agent-plan add-task <plan_id> "UI follow-up" \
  --parent <epic_task_id> \
  --depends-on <api_task_id>
` + "```" + `
`)
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
