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
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"update-task", "--dir", root, planID, "B", "--parent", taskA.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update-task parent code = %d stderr=%s", code, stderr.String())
	}
	var reparented TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &reparented); err != nil {
		t.Fatalf("Unmarshal(update-task parent) error = %v", err)
	}
	if reparented.ParentID != taskA.ID {
		t.Fatalf("reparented ParentID = %q, want %q", reparented.ParentID, taskA.ID)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"update-task", "--dir", root, planID, "B", "--parent", "", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update-task clear parent code = %d stderr=%s", code, stderr.String())
	}
	var rootTask TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &rootTask); err != nil {
		t.Fatalf("Unmarshal(update-task clear parent) error = %v", err)
	}
	if rootTask.ParentID != "" {
		t.Fatalf("rootTask ParentID = %q, want empty", rootTask.ParentID)
	}
}

func TestCLIImportDryRunApplyAndRerun(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "Import Plan", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code = %d stderr=%s", code, stderr.String())
	}
	var created Plan
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal(create) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	spec := `{
	  "tasks": [
	    {"key": "phase", "name": "Phase 1", "description": "Parent task"},
	    {"key": "design", "name": "Design", "parent": "phase"},
	    {"key": "impl", "name": "Implement", "parent": "phase", "depends_on": ["design"], "claimed_by": "sess-1"},
	    {"key": "verify", "name": "Verify", "parent": "phase"}
	  ],
	  "chains": [["impl", "verify"]]
	}`

	if code := runCLI(context.Background(), "agent-plan", []string{"import", "--dir", root, created.ID, "--dry-run", "--json"}, strings.NewReader(spec), &stdout, &stderr); code != 0 {
		t.Fatalf("import dry-run code = %d stderr=%s", code, stderr.String())
	}
	var dryRun BulkImportReport
	if err := json.Unmarshal(stdout.Bytes(), &dryRun); err != nil {
		t.Fatalf("Unmarshal(dry-run) error = %v", err)
	}
	if !dryRun.DryRun || dryRun.Created != 4 {
		t.Fatalf("dryRun = %#v", dryRun)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"show", "--dir", root, created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("show after dry-run code = %d stderr=%s", code, stderr.String())
	}
	var shown Plan
	if err := json.Unmarshal(stdout.Bytes(), &shown); err != nil {
		t.Fatalf("Unmarshal(show after dry-run) error = %v", err)
	}
	if len(shown.Tasks) != 0 {
		t.Fatalf("dry-run mutated plan: %#v", shown.Tasks)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"import", "--dir", root, created.ID, "--json"}, strings.NewReader(spec), &stdout, &stderr); code != 0 {
		t.Fatalf("import apply code = %d stderr=%s", code, stderr.String())
	}
	var applied BulkImportReport
	if err := json.Unmarshal(stdout.Bytes(), &applied); err != nil {
		t.Fatalf("Unmarshal(apply) error = %v", err)
	}
	if applied.Created != 4 || applied.Updated != 0 || applied.Unchanged != 0 {
		t.Fatalf("applied = %#v", applied)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"import", "--dir", root, created.ID, "--json"}, strings.NewReader(spec), &stdout, &stderr); code != 0 {
		t.Fatalf("import rerun code = %d stderr=%s", code, stderr.String())
	}
	var rerun BulkImportReport
	if err := json.Unmarshal(stdout.Bytes(), &rerun); err != nil {
		t.Fatalf("Unmarshal(rerun) error = %v", err)
	}
	if rerun.Created != 0 || rerun.Updated != 0 || rerun.Unchanged != 4 {
		t.Fatalf("rerun = %#v", rerun)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"show", "--dir", root, created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("show code = %d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &shown); err != nil {
		t.Fatalf("Unmarshal(show) error = %v", err)
	}
	if len(shown.Tasks) != 4 {
		t.Fatalf("shown task count = %d", len(shown.Tasks))
	}
	impl := nextTaskByImportKey(t, shown, "impl")
	verify := nextTaskByImportKey(t, shown, "verify")
	if impl.ClaimedBy != "sess-1" {
		t.Fatalf("impl claimed_by = %q", impl.ClaimedBy)
	}
	if len(verify.DependsOn) != 1 || verify.DependsOn[0] != impl.ID {
		t.Fatalf("verify deps = %#v, want %s", verify.DependsOn, impl.ID)
	}
}

func TestCLIOperationalStatsAuditAndLockCommands(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "Ops Plan", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code = %d stderr=%s", code, stderr.String())
	}
	var created Plan
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal(create) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, created.ID, "Duplicate", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add-task first code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, created.ID, "Duplicate", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("add-task second code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"stats", "--dir", root, created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("stats code = %d stderr=%s", code, stderr.String())
	}
	var stats PlanStats
	if err := json.Unmarshal(stdout.Bytes(), &stats); err != nil {
		t.Fatalf("Unmarshal(stats) error = %v", err)
	}
	if stats.Total != 2 || stats.ReadyCount != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"audit", "--dir", root, created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("audit code = %d stderr=%s", code, stderr.String())
	}
	var audit AuditReport
	if err := json.Unmarshal(stdout.Bytes(), &audit); err != nil {
		t.Fatalf("Unmarshal(audit) error = %v", err)
	}
	if !hasAuditFinding(audit, "duplicate_task_name") {
		t.Fatalf("audit findings = %#v", audit.Findings)
	}
	stdout.Reset()
	stderr.Reset()

	store, err := Open(root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	lockPayload := `{"pid":999999999,"host":"","created_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(store.planLockPath(created.ID), []byte(lockPayload), defaultFilePerm); err != nil {
		t.Fatalf("WriteFile(lock) error = %v", err)
	}

	if code := runCLI(context.Background(), "agent-plan", []string{"lock", "status", "--dir", root, created.ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("lock status code = %d stderr=%s", code, stderr.String())
	}
	var lockStatus LockStatus
	if err := json.Unmarshal(stdout.Bytes(), &lockStatus); err != nil {
		t.Fatalf("Unmarshal(lock status) error = %v", err)
	}
	if !lockStatus.Exists || lockStatus.OwnerAlive == nil || *lockStatus.OwnerAlive {
		t.Fatalf("lockStatus = %#v", lockStatus)
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"lock", "clear", "--dir", root, created.ID, "--if-dead", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("lock clear code = %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	var clearResult LockClearResult
	if err := json.Unmarshal(stdout.Bytes(), &clearResult); err != nil {
		t.Fatalf("Unmarshal(lock clear) error = %v", err)
	}
	if !clearResult.Cleared {
		t.Fatalf("clearResult = %#v", clearResult)
	}
}

func TestCLIChainCommandNarrowsReadyTasks(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()

	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "Chain Plan", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code = %d stderr=%s", code, stderr.String())
	}
	var created Plan
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal(create) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	for _, name := range []string{"A", "B", "C"} {
		if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, created.ID, name, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("add-task %s code = %d stderr=%s", name, code, stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
	}

	if code := runCLI(context.Background(), "agent-plan", []string{"chain", "--dir", root, created.ID, "A", "B", "C", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("chain code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"chain", "--dir", root, created.ID, "A", "B", "C", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("chain rerun code = %d stderr=%s", code, stderr.String())
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
	if len(ready) != 1 || ready[0]["name"] != "A" {
		t.Fatalf("ready = %#v", ready)
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
	taskA := nextTaskByName(t, shown, "A")
	taskB := nextTaskByName(t, shown, "B")
	taskC := nextTaskByName(t, shown, "C")
	if len(taskB.DependsOn) != 1 || taskB.DependsOn[0] != taskA.ID {
		t.Fatalf("taskB deps = %#v, want %s", taskB.DependsOn, taskA.ID)
	}
	if len(taskC.DependsOn) != 1 || taskC.DependsOn[0] != taskB.ID {
		t.Fatalf("taskC deps = %#v, want %s", taskC.DependsOn, taskB.ID)
	}
}

func TestCLIUpdateTaskClearDependenciesThenDependsOnReplacesDependencies(t *testing.T) {
	root := t.TempDir()
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("init code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"create", "--dir", root, "Deps Plan", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("create code = %d stderr=%s", code, stderr.String())
	}
	var created Plan
	if err := json.Unmarshal(stdout.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal(create) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()

	var tasks []TaskRecord
	for _, name := range []string{"A", "B", "C"} {
		if code := runCLI(context.Background(), "agent-plan", []string{"add-task", "--dir", root, created.ID, name, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("add-task %s code = %d stderr=%s", name, code, stderr.String())
		}
		var task TaskRecord
		if err := json.Unmarshal(stdout.Bytes(), &task); err != nil {
			t.Fatalf("Unmarshal(add-task %s) error = %v", name, err)
		}
		tasks = append(tasks, task)
		stdout.Reset()
		stderr.Reset()
	}

	if code := runCLI(context.Background(), "agent-plan", []string{"update-task", "--dir", root, created.ID, tasks[2].ID, "--depends-on", tasks[0].ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update-task initial deps code = %d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"update-task", "--dir", root, created.ID, tasks[2].ID, "--clear-dependencies", "--depends-on", tasks[1].ID, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("update-task replace deps code = %d stderr=%s", code, stderr.String())
	}
	var updated TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &updated); err != nil {
		t.Fatalf("Unmarshal(update-task replace deps) error = %v", err)
	}
	if len(updated.DependsOn) != 1 || updated.DependsOn[0] != tasks[1].ID {
		t.Fatalf("updated deps = %#v, want %s", updated.DependsOn, tasks[1].ID)
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

func TestMCPCommandUsesNewlineDelimitedMessages(t *testing.T) {
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"qa","version":"1"}}}` + "\n"

	if code := runCLI(context.Background(), "agent-plan", []string{"mcp"}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("mcp code = %d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Content-Length:") {
		t.Fatalf("mcp stdout should be newline-delimited, got %q", stdout.String())
	}
	line := strings.TrimSpace(stdout.String())
	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("Unmarshal(mcp response) error = %v stdout=%q", err, stdout.String())
	}
	if resp.JSONRPC != "2.0" || resp.ID != 1 || resp.Result.ServerInfo.Name != "agent-plan" {
		t.Fatalf("mcp response = %#v", resp)
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

func TestCLIVersion(t *testing.T) {
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"version"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("version code = %d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "agent-plan "+Version {
		t.Fatalf("version stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"--version", "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("--version --json code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"version": "`+Version+`"`) {
		t.Fatalf("--version --json stdout = %q", stdout.String())
	}
}

func nextTaskByImportKey(t *testing.T, plan Plan, key string) TaskRecord {
	t.Helper()
	for _, task := range plan.Tasks {
		if task.ImportKey == key {
			return task
		}
	}
	t.Fatalf("task with import key %q not found in %#v", key, plan.Tasks)
	return TaskRecord{}
}

func nextTaskByName(t *testing.T, plan Plan, name string) TaskRecord {
	t.Helper()
	for _, task := range plan.Tasks {
		if task.Name == name {
			return task
		}
	}
	t.Fatalf("task with name %q not found in %#v", name, plan.Tasks)
	return TaskRecord{}
}

func hasAuditFinding(report AuditReport, code string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
