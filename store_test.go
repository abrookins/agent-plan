package agentplan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreMultiPlanWorkflow(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	plans, err := store.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans() error = %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("plans = %#v", plans)
	}

	plan, err := store.CreatePlan("Backend Plan", "Track parity work", "docs/parity-design.md")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	taskA, err := store.AddTask(plan.ID, "Schema", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask(schema) error = %v", err)
	}
	taskB, err := store.AddTask(plan.ID, "Handlers", AddOptions{DependsOn: []string{taskA.ID}})
	if err != nil {
		t.Fatalf("AddTask(handlers) error = %v", err)
	}
	child, err := store.AddTask(plan.ID, "Notes", AddOptions{ParentID: taskA.ID})
	if err != nil {
		t.Fatalf("AddTask(child) error = %v", err)
	}

	loaded, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if loaded.SpecPath != "docs/parity-design.md" || len(loaded.Tasks) != 3 {
		t.Fatalf("loaded = %#v", loaded)
	}

	ready, err := store.Ready(plan.ID)
	if err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("ready = %#v", ready)
	}

	claimer := "agent-1"
	updatedChild, err := store.UpdateTask(plan.ID, child.ID, TaskUpdateOptions{
		ClaimedBy: &claimer,
	})
	if err != nil {
		t.Fatalf("UpdateTask(assign) error = %v", err)
	}
	if updatedChild.ClaimedBy != claimer {
		t.Fatalf("updatedChild = %#v", updatedChild)
	}

	claimed, err := store.ClaimNext(plan.ID, claimer)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if claimed == nil || claimed.Status != StatusInProgress || claimed.ClaimedBy != claimer {
		t.Fatalf("claimed = %#v", claimed)
	}

	deleted, err := store.DeleteTask(plan.ID, taskA.ID)
	if err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("DeleteTask() = %d, want 2", deleted)
	}

	loaded, err = store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan(after delete) error = %v", err)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].ID != taskB.ID || len(loaded.Tasks[0].DependsOn) != 0 {
		t.Fatalf("loaded after delete = %#v", loaded)
	}

	name := "Backend Plan Updated"
	desc := "Updated description"
	spec := "docs/updated.md"
	updatedPlan, err := store.UpdatePlan(plan.ID, PlanUpdateOptions{
		Name:        &name,
		Description: &desc,
		SpecPath:    &spec,
	})
	if err != nil {
		t.Fatalf("UpdatePlan() error = %v", err)
	}
	if updatedPlan.Name != name || updatedPlan.Description != desc || updatedPlan.SpecPath != spec {
		t.Fatalf("updatedPlan = %#v", updatedPlan)
	}

	if deletedPlan, err := store.DeletePlan(plan.ID); err != nil || !deletedPlan {
		t.Fatalf("DeletePlan() deleted=%v err=%v", deletedPlan, err)
	}
	plans, err = store.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans(after delete) error = %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("plans after delete = %#v", plans)
	}
}

func TestStoreImportValidationDoesNotMutate(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Import Validation", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	seed, err := store.AddTask(plan.ID, "Seed", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask(seed) error = %v", err)
	}

	report, err := store.ImportTasks(plan.ID, BulkImportSpec{
		Tasks: []BulkImportTaskSpec{
			{Key: "bad", Name: "Bad", DependsOn: []string{"missing"}},
		},
	}, BulkImportOptions{})
	if err == nil {
		t.Fatal("ImportTasks() should fail validation")
	}
	if report == nil || len(report.Errors) == 0 {
		t.Fatalf("report = %#v err=%v", report, err)
	}

	loaded, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].ID != seed.ID {
		t.Fatalf("loaded after failed import = %#v", loaded.Tasks)
	}
}

func TestStoreImportPersistenceFailureLeavesIndexedPlanUnchanged(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Import Failure", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	seed, err := store.AddTask(plan.ID, "Seed", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask(seed) error = %v", err)
	}

	originalJSONMarshalIndent := jsonMarshalIndent
	jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) {
		return nil, errors.New("forced persist failure")
	}
	_, err = store.ImportTasks(plan.ID, BulkImportSpec{
		Tasks: []BulkImportTaskSpec{
			{Key: "new", Name: "New"},
		},
	}, BulkImportOptions{})
	jsonMarshalIndent = originalJSONMarshalIndent
	if err == nil {
		t.Fatal("ImportTasks() should fail persistence")
	}

	loaded, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if len(loaded.Tasks) != 1 || loaded.Tasks[0].ID != seed.ID {
		t.Fatalf("loaded after failed persistence = %#v", loaded.Tasks)
	}
}

func TestLegacyStoreIsNotMigrated(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, metaDirName)
	if err := os.MkdirAll(filepath.Join(metaDir, tasksDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(tasks) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(metaDir, historyDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(history) error = %v", err)
	}

	now := time.Now().UTC()
	active := TaskRecord{
		ID:          "001-active",
		Sequence:    1,
		Slug:        "active",
		Name:        "Active",
		Status:      StatusNotStarted,
		Order:       0,
		CreatedAt:   now,
		UpdatedAt:   now,
		Description: "legacy active",
	}
	completeTime := now.Add(time.Minute)
	complete := TaskRecord{
		ID:          "002-done",
		Sequence:    2,
		Slug:        "done",
		Name:        "Done",
		Status:      StatusComplete,
		Order:       1,
		CreatedAt:   now,
		UpdatedAt:   completeTime,
		CompletedAt: &completeTime,
		ArchivedAt:  &completeTime,
		Description: "legacy done",
	}
	activeData, err := marshalTaskRecord(active)
	if err != nil {
		t.Fatalf("marshalTaskRecord(active) error = %v", err)
	}
	completeData, err := marshalTaskRecord(complete)
	if err != nil {
		t.Fatalf("marshalTaskRecord(complete) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, tasksDirName, active.ID+".md"), activeData, 0o644); err != nil {
		t.Fatalf("WriteFile(active) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, historyDirName, complete.ID+".done"), completeData, 0o644); err != nil {
		t.Fatalf("WriteFile(complete) error = %v", err)
	}
	legacyManifest := Manifest{
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    completeTime,
		NextSequence: 3,
		Tasks: []TaskSummary{
			{ID: active.ID, Sequence: 1, Name: active.Name, Status: active.Status, Order: 0},
			{ID: complete.ID, Sequence: 2, Name: complete.Name, Status: complete.Status, Order: 1},
		},
	}
	manifestData, err := json.MarshalIndent(legacyManifest, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(legacy manifest) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(metaDir, manifestFileName), manifestData, 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}

	if _, err := Open(root); !errors.Is(err, ErrPlanNotInitialized) {
		t.Fatalf("Open() error = %v, want %v", err, ErrPlanNotInitialized)
	}
}

func TestStoreHelpersAndGuidance(t *testing.T) {
	if got := slugify("Ab C-1"); got != "ab-c-1" {
		t.Fatalf("slugify() = %q", got)
	}
	if got := preview(strings.Repeat("a", 140)); !strings.HasSuffix(got, "...") {
		t.Fatalf("preview() = %q", got)
	}
	if got := normalizeIDs([]string{" a ", "a", "", "b"}); len(got) != 2 {
		t.Fatalf("normalizeIDs() = %#v", got)
	}

	root := t.TempDir()
	if _, err := Init(filepath.Join(root, metaDirName)); err != nil {
		t.Fatalf("Init(meta dir) error = %v", err)
	}
	if _, err := Open(filepath.Join(root, "nested")); err != nil {
		t.Fatalf("Open(nested) error = %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git) error = %v", err)
	}
	child := filepath.Join(repo, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("MkdirAll(child) error = %v", err)
	}
	if err := os.Chdir(child); err != nil {
		t.Fatalf("Chdir(child) error = %v", err)
	}
	resolved, err := normalizeRoot("")
	expected, _ := filepath.EvalSymlinks(child)
	actual, _ := filepath.EvalSymlinks(resolved)
	if err != nil || actual != expected {
		t.Fatalf("normalizeRoot(\"\") = %q err=%v", resolved, err)
	}

	path, err := InstallGuidance("AGENTS.md", false, repo)
	if err != nil {
		t.Fatalf("InstallGuidance(AGENTS) error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(AGENTS) error = %v", err)
	}
	if !strings.Contains(string(data), "Agent Plan Management") {
		t.Fatalf("AGENTS.md = %q", string(data))
	}
	path, err = InstallGuidance("skill", false, repo)
	if err != nil {
		t.Fatalf("InstallGuidance(skill) error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("skill not written: %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(skill) error = %v", err)
	}
	if !strings.HasPrefix(string(data), "---\n") {
		t.Fatalf("skill = %q", string(data))
	}
	if _, err := InstallGuidance("bad", false, repo); err == nil {
		t.Fatal("InstallGuidance(bad) should fail")
	}
}

func TestInstallGuidanceReplacesLegacyManagedBlock(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "AGENTS.md")
	existing := strings.Join([]string{
		"# Existing Instructions",
		"",
		legacyCodexGuidanceStart,
		"## Agent Dashboard Plan Management",
		"",
		"Old generated guidance.",
		legacyCodexGuidanceEnd,
		"",
		"Keep this footer.",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(existing), defaultFilePerm); err != nil {
		t.Fatalf("WriteFile(AGENTS) error = %v", err)
	}

	installedPath, err := InstallGuidance("AGENTS.md", false, repo)
	if err != nil {
		t.Fatalf("InstallGuidance(AGENTS) error = %v", err)
	}
	if installedPath != path {
		t.Fatalf("InstallGuidance path = %q, want %q", installedPath, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(AGENTS) error = %v", err)
	}
	content := string(data)
	if strings.Contains(content, "agent-dashboard:codex-guidance") {
		t.Fatalf("legacy marker was not replaced: %q", content)
	}
	if strings.Contains(content, "Old generated guidance.") {
		t.Fatalf("legacy guidance body was not replaced: %q", content)
	}
	if strings.Count(content, codexGuidanceStart) != 1 {
		t.Fatalf("codex start marker count = %d content=%q", strings.Count(content, codexGuidanceStart), content)
	}
	if strings.Count(content, codexGuidanceEnd) != 1 {
		t.Fatalf("codex end marker count = %d content=%q", strings.Count(content, codexGuidanceEnd), content)
	}
	if !strings.Contains(content, "# Existing Instructions") || !strings.Contains(content, "Keep this footer.") {
		t.Fatalf("surrounding content was not preserved: %q", content)
	}

	if _, err := InstallGuidance("AGENTS.md", false, repo); err != nil {
		t.Fatalf("second InstallGuidance(AGENTS) error = %v", err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(AGENTS second) error = %v", err)
	}
	content = string(data)
	if strings.Count(content, codexGuidanceStart) != 1 || strings.Count(content, codexGuidanceEnd) != 1 {
		t.Fatalf("reinstall duplicated managed guidance: %q", content)
	}
}

func TestMainNamedHelp(t *testing.T) {
	var stdout strings.Builder
	var stderr strings.Builder
	if code := MainNamed(context.Background(), "custom-plan", []string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("MainNamed(help) code = %d", code)
	}
	if !strings.Contains(stdout.String(), "custom-plan create") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
