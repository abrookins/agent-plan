package agentplan

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIPlanCommandsAndFlagReordering(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "API Plan", "--description", "Track API parity", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code = %d stderr=%s", code, stderr.String())
	}
	var created Plan
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal(create) error = %v", err)
	}
	if created.Name != "API Plan" {
		t.Fatalf("created = %#v", created)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"list", "--dir", root, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("list code = %d stderr=%s", code, stderr.String())
	}
	var plans []PlanSummary
	if err := json.Unmarshal(stdout.Bytes(), &plans); err != nil {
		t.Fatalf("Unmarshal(list) error = %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %#v", plans)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, created.ID, "Design store", "--description", "Map filesystem layout", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add-task code = %d stderr=%s", code, stderr.String())
	}
	var design TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &design); err != nil {
		t.Fatalf("Unmarshal(add-task) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, created.ID, "Implement store", "--depends-on", design.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add-task(dep) code = %d stderr=%s", code, stderr.String())
	}
	var impl TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &impl); err != nil {
		t.Fatalf("Unmarshal(add-task dep) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"ready", "--dir", root, created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("ready code = %d stderr=%s", code, stderr.String())
	}
	var ready []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &ready); err != nil {
		t.Fatalf("Unmarshal(ready) error = %v", err)
	}
	if len(ready) != 1 || ready[0]["id"] != design.ID {
		t.Fatalf("ready = %#v", ready)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"update-task", "--dir", root, created.ID, design.ID, "--claimed-by", "agent-cli", "--status", "in_progress", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update-task code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"next", "--dir", root, created.ID, "--by", "agent-cli", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("next code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), design.ID) {
		t.Fatalf("next stdout = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"claim-next", "--dir", root, created.ID, "--by", "agent-cli", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("claim-next code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), design.ID) {
		t.Fatalf("claim-next stdout = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo) error = %v", err)
	}
	if code := runCLI(context.Background(), "agent-plan", []string{"install", "AGENTS.md", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("install code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "AGENTS.md") {
		t.Fatalf("install stdout = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"get", "--dir", root, created.ID}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("get code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "API Plan") || !strings.Contains(stdout.String(), "Design store") {
		t.Fatalf("get stdout = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"show", "--dir", root, created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("show code = %d stderr=%s", code, stderr.String())
	}
	var shown Plan
	if err := json.Unmarshal(stdout.Bytes(), &shown); err != nil {
		t.Fatalf("Unmarshal(show) error = %v", err)
	}
	if shown.ID != created.ID || len(shown.Tasks) != 2 {
		t.Fatalf("show = %#v", shown)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"delete", "--dir", root, created.ID, "--yes", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("delete code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"deleted": true`) {
		t.Fatalf("delete stdout = %q", stdout.String())
	}
	_ = impl
}

func TestCLIRemovesImplicitPlanCommands(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d stderr=%s", code, stderr.String())
	}
	for _, args := range [][]string{
		{"add", "--dir", root, "Legacy task"},
		{"claim", "--dir", root, "001-task"},
		{"done", "--dir", root, "001-task"},
		{"block", "--dir", root, "001-task", "--reason", "blocked"},
		{"list-tasks", "--dir", root},
		{"next", "--dir", root, "plan", "--session", "sess"},
		{"bind-current", "--dir", root, "plan"},
		{"plan", "show", "--dir", root},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := runCLI(context.Background(), "agent-plan", args, strings.NewReader(""), &stdout, &stderr); code != 1 {
			t.Fatalf("%v code = %d stderr=%s", args, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "unknown command") &&
			!strings.Contains(stderr.String(), "usage: agent-plan plan show") &&
			!strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("%v stderr = %q", args, stderr.String())
		}
	}
}

func TestCLIUpdateTaskOrderReturnsUpdatedTask(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "Order Plan"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code = %d stderr=%s", code, stderr.String())
	}
	planID := strings.TrimSpace(strings.TrimPrefix(strings.Split(strings.TrimSpace(stdout.String()), "\n")[0], "Created plan: "))
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, planID, "A", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add-task A code = %d stderr=%s", code, stderr.String())
	}
	var taskA TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &taskA); err != nil {
		t.Fatalf("Unmarshal(add-task A) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, planID, "B", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add-task B code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"update-task", "--dir", root, planID, taskA.ID, "--order", "50", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update-task order code = %d stderr=%s", code, stderr.String())
	}
	var updated TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &updated); err != nil {
		t.Fatalf("Unmarshal(update-task) error = %v", err)
	}
	if updated.ID != taskA.ID {
		t.Fatalf("updated task id = %q, want %q", updated.ID, taskA.ID)
	}
}

func TestCLIConfirmDeleteAndErrorPaths(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "Delete Me"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Created plan:") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	parts := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	planID := strings.TrimSpace(strings.TrimPrefix(parts[0], "Created plan: "))
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"delete", "--dir", root, planID}, strings.NewReader("n\n"), &stdout, &stderr); code != 0 {
		t.Fatalf("delete cancel code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Cancelled.") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	t.Setenv("AGENT_PLAN_NONINTERACTIVE", "1")
	if code := runCLI(context.Background(), "agent-plan", []string{"delete", "--dir", root, planID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("delete noninteractive code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"deleted": true`) {
		t.Fatalf("delete noninteractive stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "Delete Me Again"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create second plan code = %d stderr=%s", code, stderr.String())
	}
	parts = strings.Split(strings.TrimSpace(stdout.String()), "\n")
	planID = strings.TrimSpace(strings.TrimPrefix(parts[0], "Created plan: "))

	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"claim-next", "--dir", root, planID, "--by", "agent-test"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("claim-next empty code = %d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"update-task", "--dir", root, planID, "missing", "--status", "wat"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("update-task invalid code = %d stderr=%s", code, stderr.String())
	}
}

func TestReorderFlags(t *testing.T) {
	args := reorderFlags([]string{"default", "Task", "--description", "alpha", "--json"}, map[string]bool{
		"--description": true,
		"--json":        false,
	})
	got := strings.Join(args, " ")
	if got != "--description alpha --json default Task" {
		t.Fatalf("reorderFlags() = %q", got)
	}
}

func TestInstallWritesWorkspaceFiles(t *testing.T) {
	workspace := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("Chdir(workspace) error = %v", err)
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	if code := runCLI(context.Background(), "agent-plan", []string{"install", "skill", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("install skill code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "SKILL.md") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	skillPath := filepath.Join(workspace, ".codex", "skills", planManagementSkillSlug, "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("workspace skill missing: %v", err)
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("ReadFile(skill) error = %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatalf("skill missing frontmatter: %q", string(data))
	}
}

func TestInstallSupportsDirFlag(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"install", "--dir", root, "AGENTS.md", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("install --dir code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "AGENTS.md") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("AGENTS.md missing: %v", err)
	}
}

func TestCLISupportsShowAndEnvDirFallback(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	t.Setenv("AGENT_PLAN_DIR", root)
	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init env dir code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"create", "Case Plan", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create env dir code = %d stderr=%s", code, stderr.String())
	}
	var created Plan
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal(create env dir) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"show", created.Name, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("show env dir code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"get", strings.ToLower(created.Name)}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("get case mismatch code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "expected plan ID or exact plan name") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestMCPCommandRejectsExtraArgs(t *testing.T) {
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"mcp", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("mcp extra args code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "usage: agent-plan mcp") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCLIHelpTextIsMeaningful(t *testing.T) {
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("help code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "AGENT_PLAN_DIR") || !strings.Contains(stdout.String(), "show") {
		t.Fatalf("help stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"help", "update-task"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("help update-task code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not_started, in_progress, blocked, complete, cancelled") {
		t.Fatalf("update-task help stdout = %q", stdout.String())
	}
}
