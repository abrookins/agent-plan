package agentplan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestStorePathAliasMutationPreservesAllDurableRecords(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "workspace")
	if err := os.MkdirAll(realRoot, defaultDirPerm); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v", err)
	}
	aliasRoot := filepath.Join(base, "workspace-alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	aliasStore, err := Init(aliasRoot)
	if err != nil {
		t.Fatalf("Init(alias) error = %v", err)
	}
	plan, err := aliasStore.CreatePlan("Alias Safety", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}

	spec := BulkImportSpec{Tasks: make([]BulkImportTaskSpec, 0, 350)}
	for i := 0; i < 350; i++ {
		key := fmt.Sprintf("task-%03d", i)
		description := strings.Repeat(fmt.Sprintf("description-%03d ", i), 8)
		task := BulkImportTaskSpec{
			Key:         key,
			Name:        fmt.Sprintf("Task %03d", i),
			Description: &description,
		}
		if i > 0 && i%7 == 0 {
			parent := "task-000"
			task.Parent = &parent
		}
		if i > 0 && i%5 == 0 {
			task.DependsOn = []string{fmt.Sprintf("task-%03d", i-1)}
		}
		if i%11 == 0 {
			task.Status = string(StatusComplete)
		} else if i%13 == 0 {
			task.Status = string(StatusBlocked)
		} else if i%17 == 0 {
			task.Status = string(StatusCancelled)
		} else if i%19 == 0 {
			claimer := "alias-regression"
			task.ClaimedBy = &claimer
			task.Status = string(StatusInProgress)
		}
		spec.Tasks = append(spec.Tasks, task)
	}
	if _, err := aliasStore.ImportTasks(plan.ID, spec, BulkImportOptions{}); err != nil {
		t.Fatalf("ImportTasks() error = %v", err)
	}
	if err := aliasStore.withPlanLock(plan.ID, func() error {
		loaded, manifest, err := aliasStore.loadPlan(plan.ID)
		if err != nil {
			return err
		}
		for i := range loaded.Tasks {
			loaded.Tasks[i].Output = strings.Repeat("result-"+strconv.Itoa(i)+" ", 12)
		}
		return aliasStore.persistPlanLocked(loaded, manifest, nil)
	}); err != nil {
		t.Fatalf("persist outputs error = %v", err)
	}

	before, err := aliasStore.loadManifest(plan.ID)
	if err != nil {
		t.Fatalf("loadManifest(before) error = %v", err)
	}
	beforeChecksums := manifestRecordChecksums(t, aliasStore, plan.ID, before)

	realStore, err := Open(realRoot)
	if err != nil {
		t.Fatalf("Open(realRoot) error = %v", err)
	}
	if realStore.Root != aliasStore.Root {
		t.Fatalf("canonical roots differ: alias=%q real=%q", aliasStore.Root, realStore.Root)
	}

	targetID := before.Tasks[1].ID
	complete := StatusComplete
	if _, err := realStore.UpdateTask(plan.ID, targetID, TaskUpdateOptions{Status: &complete}); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}

	afterPlan, err := realStore.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan(after alias mutation) error = %v", err)
	}
	if len(afterPlan.Tasks) != 350 {
		t.Fatalf("task count after alias mutation = %d, want 350", len(afterPlan.Tasks))
	}
	after, err := realStore.loadManifest(plan.ID)
	if err != nil {
		t.Fatalf("loadManifest(after) error = %v", err)
	}
	if after.Version != 2 {
		t.Fatalf("manifest version = %d, want 2", after.Version)
	}
	for _, summary := range after.Tasks {
		if filepath.IsAbs(summary.Path) || filepath.IsAbs(summary.ClaimPath) {
			t.Fatalf("manifest retained absolute path: %#v", summary)
		}
		if summary.ID == targetID {
			if !strings.HasPrefix(summary.Path, historyDirName+string(filepath.Separator)) {
				t.Fatalf("completed task path = %q, want history path", summary.Path)
			}
			continue
		}
		got := fileChecksum(t, resolveManifestTestPath(realStore, plan.ID, summary.Path))
		if want := beforeChecksums[summary.ID]; got != want {
			t.Fatalf("checksum changed for unaffected task %s: got %x want %x", summary.ID, got, want)
		}
	}
	if _, err := os.Stat(realStore.taskPath(plan.ID, targetID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed task still exists under tasks: %v", err)
	}
	if _, err := os.Stat(realStore.historyPath(plan.ID, targetID)); err != nil {
		t.Fatalf("completed task history record missing: %v", err)
	}
	if _, err := realStore.Stats(plan.ID); err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if _, err := realStore.Ready(plan.ID); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	if _, err := realStore.Audit(plan.ID); err != nil {
		t.Fatalf("Audit() error = %v", err)
	}
}

func TestPersistenceFailureNeverLeavesManifestReferencingMissingRecord(t *testing.T) {
	for _, failDestination := range []string{planFileName, manifestFileName} {
		t.Run(failDestination, func(t *testing.T) {
			root := t.TempDir()
			store, err := Init(root)
			if err != nil {
				t.Fatalf("Init() error = %v", err)
			}
			plan, err := store.CreatePlan("Fault Injection", "", "")
			if err != nil {
				t.Fatalf("CreatePlan() error = %v", err)
			}
			task, err := store.AddTask(plan.ID, "Move me", AddOptions{})
			if err != nil {
				t.Fatalf("AddTask() error = %v", err)
			}
			if _, err := store.AddTask(plan.ID, "Keep me", AddOptions{}); err != nil {
				t.Fatalf("AddTask(keep) error = %v", err)
			}

			originalRename := renameFunc
			renameFunc = func(oldPath, newPath string) error {
				if filepath.Base(newPath) == failDestination {
					return errors.New("injected commit failure")
				}
				return originalRename(oldPath, newPath)
			}
			t.Cleanup(func() {
				renameFunc = originalRename
			})

			complete := StatusComplete
			if _, err := store.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{Status: &complete}); err == nil {
				t.Fatal("UpdateTask() unexpectedly succeeded")
			}
			assertManifestReferencesReadableRecords(t, store, plan.ID)
		})
	}
}

func TestTaskRecordWriteFailureKeepsOldManifestReadable(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Record Failure", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := store.AddTask(plan.ID, "Move me", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	originalRename := renameFunc
	renameFunc = func(oldPath, newPath string) error {
		if strings.HasSuffix(newPath, ".done") {
			return errors.New("injected task record failure")
		}
		return originalRename(oldPath, newPath)
	}
	t.Cleanup(func() {
		renameFunc = originalRename
	})

	complete := StatusComplete
	if _, err := store.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{Status: &complete}); err == nil {
		t.Fatal("UpdateTask() unexpectedly succeeded")
	}
	assertManifestReferencesReadableRecords(t, store, plan.ID)
}

func TestClaimPersistenceFailureRemovesUncommittedClaim(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Claim Failure", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := store.AddTask(plan.ID, "Claim me", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	originalRename := renameFunc
	renameFunc = func(oldPath, newPath string) error {
		if filepath.Base(newPath) == planFileName {
			return errors.New("injected plan commit failure")
		}
		return originalRename(oldPath, newPath)
	}
	t.Cleanup(func() {
		renameFunc = originalRename
	})

	if _, err := store.ClaimNext(plan.ID, "claim-failure"); err == nil {
		t.Fatal("ClaimNext() unexpectedly succeeded")
	}
	if _, err := os.Stat(store.claimPath(plan.ID, task.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("uncommitted claim still exists: %v", err)
	}
	loaded, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if loaded.GetTask(task.ID) == nil {
		t.Fatalf("failed claim removed durable task %s", task.ID)
	}
}

func TestCleanupFailureLeavesAuditableOrphanAfterCommittedMutation(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Cleanup Failure", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := store.AddTask(plan.ID, "Move me", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	oldPath := store.taskPath(plan.ID, task.ID)

	originalRemove := removeFileFunc
	removeFileFunc = func(path string) error {
		if path == oldPath {
			return errors.New("injected cleanup failure")
		}
		return originalRemove(path)
	}
	t.Cleanup(func() {
		removeFileFunc = originalRemove
	})

	complete := StatusComplete
	if _, err := store.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{Status: &complete}); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if _, err := store.GetPlan(plan.ID); err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("orphan was not retained: %v", err)
	}
	if warnings := store.Warnings(); len(warnings) == 0 || !strings.Contains(warnings[len(warnings)-1], "orphan") {
		t.Fatalf("cleanup warnings = %#v", warnings)
	}
}

func TestMutationWritesOnlyChangedTaskRecords(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Change Set", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	changed, err := store.AddTask(plan.ID, "Changed", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask(changed) error = %v", err)
	}
	if _, err := store.AddTask(plan.ID, "Untouched", AddOptions{}); err != nil {
		t.Fatalf("AddTask(untouched) error = %v", err)
	}
	loadedBefore, manifestBefore, err := store.loadPlan(plan.ID)
	if err != nil {
		t.Fatalf("loadPlan(before) error = %v", err)
	}
	for i := range loadedBefore.Tasks {
		if !taskSummariesEqual(manifestBefore.Tasks[i], summaryFromRecord(loadedBefore.Tasks[i])) {
			t.Fatalf("pre-mutation summary mismatch:\nmanifest=%#v\nrecord=%#v", manifestBefore.Tasks[i], summaryFromRecord(loadedBefore.Tasks[i]))
		}
	}

	originalRename := renameFunc
	recordWrites := 0
	renameFunc = func(oldPath, newPath string) error {
		if strings.HasSuffix(newPath, ".md") || strings.HasSuffix(newPath, ".done") {
			recordWrites++
		}
		return originalRename(oldPath, newPath)
	}
	t.Cleanup(func() {
		renameFunc = originalRename
	})

	description := "changed once"
	if _, err := store.UpdateTask(plan.ID, changed.ID, TaskUpdateOptions{Description: &description}); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	if recordWrites != 1 {
		t.Fatalf("record writes = %d, want 1", recordWrites)
	}
}

func TestChangedRecordAndClaimCommitBeforeManifest(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Commit Ordering", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := store.AddTask(plan.ID, "Claimed", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	originalRename := renameFunc
	destinations := make([]string, 0)
	renameFunc = func(oldPath, newPath string) error {
		destinations = append(destinations, filepath.Base(newPath))
		return originalRename(oldPath, newPath)
	}
	t.Cleanup(func() {
		renameFunc = originalRename
	})

	claimer := "ordering-test"
	description := "changed before manifest"
	if _, err := store.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{
		Description: &description,
		ClaimedBy:   &claimer,
	}); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	recordIndex := indexOfDestination(destinations, task.ID+".md")
	claimIndex := indexOfDestination(destinations, task.ID+".claim")
	manifestIndex := indexOfDestination(destinations, manifestFileName)
	if recordIndex < 0 || claimIndex < 0 || manifestIndex < 0 {
		t.Fatalf("commit destinations missing: %#v", destinations)
	}
	if recordIndex > manifestIndex || claimIndex > manifestIndex {
		t.Fatalf("record or claim committed after manifest: %#v", destinations)
	}
}

func TestMutationResultsRetainRequestedIdentityAcrossSorting(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Mutation Identity", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	spec := BulkImportSpec{Tasks: make([]BulkImportTaskSpec, 0, 1000)}
	for i := 0; i < 1000; i++ {
		order := 1000 - i
		spec.Tasks = append(spec.Tasks, BulkImportTaskSpec{
			Key:   fmt.Sprintf("seed-%04d", i),
			Name:  fmt.Sprintf("Seed %04d", i),
			Order: &order,
		})
	}
	if _, err := store.ImportTasks(plan.ID, spec, BulkImportOptions{}); err != nil {
		t.Fatalf("ImportTasks(seed) error = %v", err)
	}

	firstOrder := 0
	created, err := store.AddTask(plan.ID, "Created at front", AddOptions{Order: &firstOrder})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	if created.Name != "Created at front" || created.ID != "1001-created-at-front" {
		t.Fatalf("AddTask() returned wrong task: %#v", created)
	}
	assertMutationIdentity(t, store, plan.ID, created)

	lastOrder := 2000
	updated, err := store.UpdateTask(plan.ID, created.ID, TaskUpdateOptions{Order: &lastOrder})
	if err != nil {
		t.Fatalf("UpdateTask(order) error = %v", err)
	}
	assertMutationIdentity(t, store, plan.ID, updated)

	description := "updated through import"
	report, err := store.ImportTasks(plan.ID, BulkImportSpec{Tasks: []BulkImportTaskSpec{{
		ID:          created.ID,
		Name:        created.Name,
		Description: &description,
		Order:       &firstOrder,
	}}}, BulkImportOptions{})
	if err != nil {
		t.Fatalf("ImportTasks(update) error = %v", err)
	}
	if report.Updated != 1 || len(report.Changes) != 1 || report.Changes[0].ID != created.ID {
		t.Fatalf("import report identity = %#v", report)
	}

	loaded, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	chainIDs := []string{loaded.Tasks[0].ID, loaded.Tasks[1].ID, loaded.Tasks[2].ID}
	chained, err := store.ChainTasks(plan.ID, chainIDs)
	if err != nil {
		t.Fatalf("ChainTasks() error = %v", err)
	}
	for i, task := range chained {
		if task.ID != chainIDs[i] {
			t.Fatalf("chain result[%d] = %s, want %s", i, task.ID, chainIDs[i])
		}
		assertMutationIdentity(t, store, plan.ID, &task)
	}

	claimed, err := store.ClaimNext(plan.ID, "identity-test")
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimNext() returned nil")
	}
	assertMutationIdentity(t, store, plan.ID, claimed)
	complete := StatusComplete
	completed, err := store.UpdateTask(plan.ID, claimed.ID, TaskUpdateOptions{Status: &complete})
	if err != nil {
		t.Fatalf("UpdateTask(complete) error = %v", err)
	}
	assertMutationIdentity(t, store, plan.ID, completed)
}

func TestClaimNextPromotesOwnedReadyTaskToInProgress(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Claim Promotion", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := store.AddTask(plan.ID, "Owned work", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	claimer := "claim-promotion-test"
	owned, err := store.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{ClaimedBy: &claimer})
	if err != nil {
		t.Fatalf("UpdateTask(claimed_by) error = %v", err)
	}
	if owned.Status != StatusNotStarted || owned.ClaimedBy != claimer {
		t.Fatalf("owned task = %#v", owned)
	}
	claimedAt := owned.ClaimedAt
	beforeClaim, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan(before claim) error = %v", err)
	}

	claimed, err := store.ClaimNext(plan.ID, claimer)
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if claimed == nil {
		t.Fatal("ClaimNext() returned nil")
	}
	if claimed.ID != task.ID ||
		claimed.Status != StatusInProgress ||
		claimed.ClaimedBy != claimer ||
		claimed.Assignee != claimer {
		t.Fatalf("claimed task = %#v", claimed)
	}
	if claimedAt == nil || claimed.ClaimedAt == nil || !claimed.ClaimedAt.Equal(*claimedAt) {
		t.Fatalf("claim timestamp changed: before=%v after=%v", claimedAt, claimed.ClaimedAt)
	}
	afterClaim, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan(after claim) error = %v", err)
	}
	if afterClaim.Version != beforeClaim.Version+1 {
		t.Fatalf("claim version = %d, want %d", afterClaim.Version, beforeClaim.Version+1)
	}

	repeated, err := store.ClaimNext(plan.ID, claimer)
	if err != nil {
		t.Fatalf("ClaimNext(repeated) error = %v", err)
	}
	if repeated == nil || repeated.ID != task.ID || repeated.Status != StatusInProgress {
		t.Fatalf("repeated claim = %#v", repeated)
	}
	afterRepeated, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan(after repeated claim) error = %v", err)
	}
	if afterRepeated.Version != afterClaim.Version {
		t.Fatalf("repeated claim version = %d, want %d", afterRepeated.Version, afterClaim.Version)
	}

	directTask, err := store.AddTask(plan.ID, "Direct claim", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask(direct claim) error = %v", err)
	}
	beforeDirect, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan(before direct claim) error = %v", err)
	}
	direct, err := store.ClaimTask(plan.ID, directTask.ID, "direct-claimer")
	if err != nil {
		t.Fatalf("ClaimTask() error = %v", err)
	}
	if direct.Status != StatusInProgress || direct.ClaimedBy != "direct-claimer" {
		t.Fatalf("direct claim = %#v", direct)
	}
	afterDirect, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan(after direct claim) error = %v", err)
	}
	if afterDirect.Version != beforeDirect.Version+1 {
		t.Fatalf("direct claim version = %d, want %d", afterDirect.Version, beforeDirect.Version+1)
	}
}

func TestTaskQueryValidationUsesInvalidArgumentErrors(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)
	created := server.CallTool(context.Background(), "plan_create", map[string]any{
		"name": "Invalid Arguments",
	})
	if created.IsError {
		t.Fatalf("plan_create result = %#v", created)
	}
	planMap := regressionMap(t, created.StructuredContent)
	planID, _ := planMap["id"].(string)

	mcpResult := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan": planID,
	})
	if !mcpResult.IsError {
		t.Fatalf("plan_get_tasks without filters unexpectedly succeeded: %#v", mcpResult)
	}
	mcpPayload := regressionMap(t, mcpResult.StructuredContent)
	mcpError := mcpPayload["error"].(map[string]any)
	if code, _ := mcpError["code"].(string); code != ErrorCodeInvalidArgument {
		t.Fatalf("MCP error code = %q, want %q", code, ErrorCodeInvalidArgument)
	}

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{
			name: "missing filter",
			args: []string{"get-task", "--dir", root, planID, "--json"},
		},
		{
			name: "invalid status",
			args: []string{"get-task", "--dir", root, planID, "--status", "bogus", "--json"},
		},
		{
			name: "unknown field",
			args: []string{"get-task", "--dir", root, planID, "missing-task", "--fields", "id,secret", "--json"},
		},
		{
			name: "negative known version",
			args: []string{"get-task", "--dir", root, planID, "--status", "not_started", "--known-version", "-2", "--json"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			stdout := bytes.Buffer{}
			stderr := bytes.Buffer{}
			code := runCLI(
				context.Background(),
				"agent-plan",
				testCase.args,
				strings.NewReader(""),
				&stdout,
				&stderr,
			)
			if code != 1 {
				t.Fatalf("code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			var envelope ErrorEnvelope
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("Unmarshal(error) = %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
			}
			if envelope.Error.Code != ErrorCodeInvalidArgument {
				t.Fatalf(
					"error code = %q, want %q; stdout=%q stderr=%q",
					envelope.Error.Code,
					ErrorCodeInvalidArgument,
					stdout.String(),
					stderr.String(),
				)
			}
		})
	}
}

func TestMissingTaskRecordHasTypedErrorAndDegradedDiagnostics(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Degraded", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := store.AddTask(plan.ID, "Missing", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	if err := os.Remove(store.taskPath(plan.ID, task.ID)); err != nil {
		t.Fatalf("Remove(task) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.planTasksDir(plan.ID), "orphan.md"), []byte("orphan"), defaultFilePerm); err != nil {
		t.Fatalf("WriteFile(orphan task) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(store.planTasksDir(plan.ID), "orphan.claim"), []byte(`{"by":"nobody"}`), defaultFilePerm); err != nil {
		t.Fatalf("WriteFile(orphan claim) error = %v", err)
	}

	_, err = store.GetPlan(plan.ID)
	if got := regressionErrorCode(err); got != "task_record_missing" {
		t.Fatalf("GetPlan() code = %q err=%v, want task_record_missing", got, err)
	}
	report, err := store.Audit(plan.ID)
	if err != nil {
		t.Fatalf("Audit() error = %v", err)
	}
	for _, code := range []string{"missing_task_record", "orphan_task_record", "orphan_claim"} {
		if !hasAuditFinding(report, code) {
			t.Errorf("Audit() missing %q: %#v", code, report.Findings)
		}
	}
	stats, err := store.Stats(plan.ID)
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	encoded, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("Marshal(stats) error = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("Unmarshal(stats) error = %v", err)
	}
	if fields["health"] != "degraded" {
		t.Fatalf("stats health = %#v, want degraded", fields["health"])
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	code := runCLI(context.Background(), "agent-plan", []string{
		"show", "--dir", root, plan.ID, "--json",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("show unexpectedly succeeded: %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("show stderr = %q, want empty", stderr.String())
	}
	var envelope ErrorEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal(show error) error = %v output=%q", err, stdout.String())
	}
	if envelope.OK || envelope.Error.Code != ErrorCodeTaskRecordMissing || envelope.Error.TaskID != task.ID || envelope.Error.Path == "" {
		t.Fatalf("show error envelope = %#v", envelope)
	}

	result := NewMCPServer(root).CallTool(context.Background(), "plan_show", map[string]any{"plan": plan.ID})
	if !result.IsError {
		t.Fatalf("plan_show unexpectedly succeeded: %#v", result)
	}
	mcpEnvelope := regressionMap(t, result.StructuredContent)
	nestedError, _ := mcpEnvelope["error"].(map[string]any)
	if nestedError["code"] != ErrorCodeTaskRecordMissing || nestedError["task_id"] != task.ID {
		t.Fatalf("MCP error envelope = %#v", mcpEnvelope)
	}
	for _, item := range result.Content {
		if strings.Contains(item.Text, "{") || strings.Contains(item.Text, task.ID) {
			t.Fatalf("MCP error text serialized structured error: %q", item.Text)
		}
	}
}

func TestPlanResolutionDoesNotMaskUnreadableExactIDWithNameFallback(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	corrupt, err := store.CreatePlan("Corrupt", "", "")
	if err != nil {
		t.Fatalf("CreatePlan(corrupt) error = %v", err)
	}
	task, err := store.AddTask(corrupt.ID, "Missing", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	if _, err := store.CreatePlan(corrupt.ID, "", ""); err != nil {
		t.Fatalf("CreatePlan(name fallback) error = %v", err)
	}
	if err := os.Remove(store.taskPath(corrupt.ID, task.ID)); err != nil {
		t.Fatalf("Remove(task) error = %v", err)
	}

	_, err = resolvePlan(store, corrupt.ID)
	if got := regressionErrorCode(err); got != ErrorCodeTaskRecordMissing {
		t.Fatalf("resolvePlan() code = %q err=%v, want %s", got, err, ErrorCodeTaskRecordMissing)
	}
}

func TestAuditReportsDuplicatePathsClaimMismatchAndInvalidManifest(t *testing.T) {
	t.Run("duplicate record path and claim mismatch", func(t *testing.T) {
		root := t.TempDir()
		store, err := Init(root)
		if err != nil {
			t.Fatalf("Init() error = %v", err)
		}
		plan, err := store.CreatePlan("Audit Findings", "", "")
		if err != nil {
			t.Fatalf("CreatePlan() error = %v", err)
		}
		task, err := store.AddTask(plan.ID, "Claimed", AddOptions{})
		if err != nil {
			t.Fatalf("AddTask() error = %v", err)
		}
		claimed, err := store.ClaimTask(plan.ID, task.ID, "expected")
		if err != nil {
			t.Fatalf("ClaimTask() error = %v", err)
		}
		manifest, err := store.loadManifest(plan.ID)
		if err != nil {
			t.Fatalf("loadManifest() error = %v", err)
		}
		manifest.Tasks = append(manifest.Tasks, manifest.Tasks[0])
		if err := atomicWriteJSON(store.planManifestPath(plan.ID), manifest); err != nil {
			t.Fatalf("write duplicate manifest error = %v", err)
		}
		if err := atomicWriteJSON(store.claimPath(plan.ID, claimed.ID), ClaimInfo{
			By:        "different",
			ClaimedAt: *claimed.ClaimedAt,
		}); err != nil {
			t.Fatalf("write mismatched claim error = %v", err)
		}
		report, err := store.Audit(plan.ID)
		if err != nil {
			t.Fatalf("Audit() error = %v", err)
		}
		for _, code := range []string{"duplicate_record_path", "claim_state_mismatch"} {
			if !hasAuditFinding(report, code) {
				t.Errorf("Audit() missing %q: %#v", code, report.Findings)
			}
		}
	})

	t.Run("invalid manifest", func(t *testing.T) {
		root := t.TempDir()
		store, err := Init(root)
		if err != nil {
			t.Fatalf("Init() error = %v", err)
		}
		plan, err := store.CreatePlan("Invalid Manifest", "", "")
		if err != nil {
			t.Fatalf("CreatePlan() error = %v", err)
		}
		manifest, err := store.loadManifest(plan.ID)
		if err != nil {
			t.Fatalf("loadManifest() error = %v", err)
		}
		manifest.Version = 99
		if err := atomicWriteJSON(store.planManifestPath(plan.ID), manifest); err != nil {
			t.Fatalf("write invalid manifest error = %v", err)
		}
		report, err := store.Audit(plan.ID)
		if err != nil {
			t.Fatalf("Audit() error = %v", err)
		}
		if report.Stats.Health != "unreadable" || !hasAuditFinding(report, ErrorCodeManifestInvalid) {
			t.Fatalf("Audit(invalid manifest) = %#v", report)
		}
		stats, err := store.Stats(plan.ID)
		if err != nil {
			t.Fatalf("Stats(invalid manifest) error = %v", err)
		}
		if stats.Health != "unreadable" || stats.FindingCount != 1 {
			t.Fatalf("Stats(invalid manifest) = %#v", stats)
		}
	})
}

func TestBoundedOutputContractsAndCompatibility(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)
	created := server.CallTool(context.Background(), "plan_create", map[string]any{"name": "Output Contract"})
	if created.IsError {
		t.Fatalf("plan_create result = %#v", created)
	}
	planMap := regressionMap(t, created.StructuredContent)
	planID, _ := planMap["id"].(string)
	added := server.CallTool(context.Background(), "plan_add_task", map[string]any{
		"plan":        planID,
		"name":        "Large task",
		"description": strings.Repeat("description ", 200),
	})
	if added.IsError {
		t.Fatalf("plan_add_task result = %#v", added)
	}
	for _, result := range []struct {
		name string
		got  any
	}{
		{name: "plan_create", got: created},
		{name: "plan_add_task", got: added},
	} {
		encoded, err := json.Marshal(result.got)
		if err != nil {
			t.Fatalf("Marshal(%s) error = %v", result.name, err)
		}
		_ = encoded
	}
	for _, item := range added.Content {
		if len([]byte(item.Text)) > 512 {
			t.Errorf("MCP text length = %d, want <= 512", len([]byte(item.Text)))
		}
		structured, _ := json.Marshal(added.StructuredContent)
		if bytes.Contains([]byte(item.Text), structured) || strings.Contains(item.Text, `"description"`) {
			t.Errorf("MCP text duplicates structured payload: %q", item.Text)
		}
	}

	tasks := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   planID,
		"tasks":  []string{"Large task"},
		"fields": []string{"id", "name", "status", "description_bytes"},
		"limit":  25,
	})
	if tasks.IsError {
		t.Fatalf("plan_get_tasks result = %#v", tasks)
	}
	taskPayload := regressionMap(t, tasks.StructuredContent)
	if returned, _ := taskPayload["returned"].(float64); returned != 1 {
		t.Fatalf("returned = %#v, want 1", taskPayload["returned"])
	}
	defaultTasks := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":  planID,
		"tasks": []string{"Large task"},
	})
	if defaultTasks.IsError {
		t.Fatalf("plan_get_tasks(default) result = %#v", defaultTasks)
	}
	defaultPayload := regressionMap(t, defaultTasks.StructuredContent)
	defaultItems, _ := defaultPayload["tasks"].([]any)
	defaultItem := defaultItems[0].(map[string]any)
	for _, field := range []string{"description", "output", "path", "claim_path", "created_at", "completed_at", "archived_at"} {
		if _, ok := defaultItem[field]; ok {
			t.Errorf("default task view exposed %s: %#v", field, defaultItem)
		}
	}
	explicitTasks := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   planID,
		"tasks":  []string{"Large task"},
		"fields": []string{"id", "description", "output", "path", "created_at"},
	})
	if explicitTasks.IsError {
		t.Fatalf("plan_get_tasks(explicit) result = %#v", explicitTasks)
	}
	explicitPayload := regressionMap(t, explicitTasks.StructuredContent)
	explicitItems, _ := explicitPayload["tasks"].([]any)
	explicitItem := explicitItems[0].(map[string]any)
	for _, field := range []string{"description", "output", "path", "created_at"} {
		if _, ok := explicitItem[field]; !ok {
			t.Errorf("explicit task view omitted %s: %#v", field, explicitItem)
		}
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	code := runCLI(context.Background(), "agent-plan", []string{
		"get-task", "--dir", root, planID, "Large task",
		"--fields", "id,name,status,description_bytes", "--json",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("get-task code = %d stderr=%s", code, stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("\n  ")) {
		t.Fatalf("default --json is pretty-printed: %q", stdout.String())
	}
	compactJSON := append([]byte(nil), stdout.Bytes()...)

	stdout.Reset()
	stderr.Reset()
	code = runCLI(context.Background(), "agent-plan", []string{
		"get-task", "--dir", root, planID, "Large task",
		"--fields", "id,name,status,description_bytes", "--json", "--pretty",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 || !bytes.Contains(stdout.Bytes(), []byte("\n  ")) {
		t.Fatalf("pretty get-task code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var compactValue any
	var prettyValue any
	if err := json.Unmarshal(compactJSON, &compactValue); err != nil {
		t.Fatalf("Unmarshal(compact) error = %v", err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &prettyValue); err != nil {
		t.Fatalf("Unmarshal(pretty) error = %v", err)
	}
	compactSemantic, _ := json.Marshal(compactValue)
	prettySemantic, _ := json.Marshal(prettyValue)
	if !bytes.Equal(compactSemantic, prettySemantic) {
		t.Fatalf("compact and pretty results differ:\ncompact=%s\npretty=%s", compactSemantic, prettySemantic)
	}

	stdout.Reset()
	stderr.Reset()
	code = runCLI(context.Background(), "agent-plan", []string{
		"get", "--dir", root, "missing-plan", "--json",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || bytes.Contains(stdout.Bytes(), []byte("\n  ")) {
		t.Fatalf("compact error code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	compactError := append([]byte(nil), stdout.Bytes()...)
	stdout.Reset()
	stderr.Reset()
	code = runCLI(context.Background(), "agent-plan", []string{
		"get", "--dir", root, "missing-plan", "--json", "--pretty",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !bytes.Contains(stdout.Bytes(), []byte("\n  ")) {
		t.Fatalf("pretty error code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if err := json.Unmarshal(compactError, &compactValue); err != nil {
		t.Fatalf("Unmarshal(compact error) error = %v", err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &prettyValue); err != nil {
		t.Fatalf("Unmarshal(pretty error) error = %v", err)
	}
	compactSemantic, _ = json.Marshal(compactValue)
	prettySemantic, _ = json.Marshal(prettyValue)
	if !bytes.Equal(compactSemantic, prettySemantic) {
		t.Fatalf("compact and pretty errors differ:\ncompact=%s\npretty=%s", compactSemantic, prettySemantic)
	}
}

func TestProgressiveRetrievalFiltersPaginationAndKnownVersion(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Progressive Retrieval", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	spec := BulkImportSpec{Tasks: make([]BulkImportTaskSpec, 0, 30)}
	for i := 0; i < 30; i++ {
		description := strings.Repeat(fmt.Sprintf("task-%02d ", i), 1000)
		status := string(StatusNotStarted)
		if i%2 == 0 {
			status = string(StatusComplete)
		}
		task := BulkImportTaskSpec{
			Key:         fmt.Sprintf("task-%02d", i),
			Name:        fmt.Sprintf("Task %02d", i),
			Description: &description,
			Status:      status,
		}
		if i == 1 {
			parent := "task-00"
			claimedBy := "retrieval-worker"
			task.Parent = &parent
			task.ClaimedBy = &claimedBy
			task.Status = string(StatusInProgress)
		}
		spec.Tasks = append(spec.Tasks, task)
	}
	if _, err := store.ImportTasks(plan.ID, spec, BulkImportOptions{}); err != nil {
		t.Fatalf("ImportTasks() error = %v", err)
	}
	server := NewMCPServer(root)
	defaultView := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":  plan.ID,
		"tasks": []string{"Task 01"},
	})
	if defaultView.IsError {
		t.Fatalf("plan_get_tasks(default view) result = %#v", defaultView)
	}
	defaultPayload := regressionMap(t, defaultView.StructuredContent)
	defaultTasks, _ := defaultPayload["tasks"].([]any)
	if len(defaultTasks) != 1 {
		t.Fatalf("default tasks = %#v", defaultTasks)
	}
	defaultTask := defaultTasks[0].(map[string]any)
	if _, ok := defaultTask["description"]; ok {
		t.Fatalf("default view exposed full description: %#v", defaultTask)
	}
	if _, ok := defaultTask["path"]; ok {
		t.Fatalf("default view exposed path: %#v", defaultTask)
	}
	preview, _ := defaultTask["description_preview"].(string)
	if len([]rune(preview)) > 240 {
		t.Fatalf("description preview runes = %d, want <= 240", len([]rune(preview)))
	}
	filtered := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":       plan.ID,
		"parent":     "Task 00",
		"claimed_by": "retrieval-worker",
		"status":     []string{string(StatusInProgress)},
		"fields":     []string{"id", "name", "status", "parent_id", "claimed_by"},
	})
	if filtered.IsError {
		t.Fatalf("plan_get_tasks(filters) result = %#v", filtered)
	}
	filteredPayload := regressionMap(t, filtered.StructuredContent)
	if filteredPayload["returned"] != float64(1) {
		t.Fatalf("filtered payload = %#v", filteredPayload)
	}
	invalid := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan": plan.ID,
	})
	if !invalid.IsError {
		t.Fatalf("filter-free plan_get_tasks unexpectedly succeeded: %#v", invalid)
	}
	invalid = server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   plan.ID,
		"tasks":  []string{"Task 01"},
		"limit":  101,
		"fields": []string{"id"},
	})
	if !invalid.IsError {
		t.Fatalf("over-limit plan_get_tasks unexpectedly succeeded: %#v", invalid)
	}
	first := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   plan.ID,
		"status": []string{string(StatusComplete)},
		"fields": []string{"id", "name", "status", "description_preview", "description_bytes"},
		"limit":  2,
	})
	if first.IsError {
		t.Fatalf("plan_get_tasks(first) result = %#v", first)
	}
	firstPayload := regressionMap(t, first.StructuredContent)
	if firstPayload["returned"] != float64(2) || firstPayload["next_cursor"] == nil {
		t.Fatalf("first page = %#v", firstPayload)
	}
	if encoded, _ := json.Marshal(firstPayload); len(encoded) > 64*1024 {
		t.Fatalf("first page bytes = %d, want <= 64 KiB", len(encoded))
	}
	firstTasks, _ := firstPayload["tasks"].([]any)
	for _, item := range firstTasks {
		task := item.(map[string]any)
		if _, ok := task["description"]; ok {
			t.Fatalf("default page exposed full description: %#v", task)
		}
	}

	second := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   plan.ID,
		"status": []string{string(StatusComplete)},
		"fields": []string{"id", "name", "status", "description_preview", "description_bytes"},
		"limit":  2,
		"cursor": firstPayload["next_cursor"],
	})
	if second.IsError {
		t.Fatalf("plan_get_tasks(second) result = %#v", second)
	}
	secondPayload := regressionMap(t, second.StructuredContent)
	if secondPayload["returned"] != float64(2) {
		t.Fatalf("second page = %#v", secondPayload)
	}
	bytePage := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   plan.ID,
		"status": []string{string(StatusComplete)},
		"fields": []string{"id", "description"},
		"limit":  100,
	})
	if bytePage.IsError {
		t.Fatalf("plan_get_tasks(byte page) result = %#v", bytePage)
	}
	bytePayload := regressionMap(t, bytePage.StructuredContent)
	byteEncoded, _ := json.Marshal(bytePayload)
	if len(byteEncoded) > 64*1024 {
		t.Fatalf("byte-aware page bytes = %d, want <= 64 KiB", len(byteEncoded))
	}
	if bytePayload["next_cursor"] == nil {
		t.Fatalf("byte-aware page did not paginate: %#v", bytePayload)
	}

	version := int(firstPayload["version"].(float64))
	unchanged := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":          plan.ID,
		"tasks":         []string{"Task 00"},
		"known_version": version,
	})
	if unchanged.IsError {
		t.Fatalf("plan_get_tasks(unchanged) result = %#v", unchanged)
	}
	unchangedPayload := regressionMap(t, unchanged.StructuredContent)
	if unchangedPayload["unchanged"] != true || len(unchangedPayload) != 3 {
		t.Fatalf("unchanged payload = %#v", unchangedPayload)
	}
	for _, toolName := range []string{"plan_stats", "plan_ready", "plan_get", "plan_show"} {
		result := server.CallTool(context.Background(), toolName, map[string]any{
			"plan":          plan.ID,
			"known_version": version,
		})
		if result.IsError {
			t.Fatalf("%s(unchanged) result = %#v", toolName, result)
		}
		payload := regressionMap(t, result.StructuredContent)
		if payload["unchanged"] != true || len(payload) != 3 {
			t.Fatalf("%s unchanged payload = %#v", toolName, payload)
		}
	}

	description := "version changed"
	loaded, err := store.GetPlan(plan.ID)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	firstTask, err := resolveTask(loaded, "Task 00", "task")
	if err != nil {
		t.Fatalf("resolveTask(Task 00) error = %v", err)
	}
	if _, err := store.UpdateTask(plan.ID, firstTask.ID, TaskUpdateOptions{Description: &description}); err != nil {
		t.Fatalf("UpdateTask() error = %v", err)
	}
	stale := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   plan.ID,
		"status": []string{string(StatusComplete)},
		"fields": []string{"id", "name", "status", "description_preview", "description_bytes"},
		"limit":  2,
		"cursor": firstPayload["next_cursor"],
	})
	if !stale.IsError {
		t.Fatalf("stale cursor unexpectedly succeeded: %#v", stale)
	}
	stalePayload := regressionMap(t, stale.StructuredContent)
	if nested, _ := stalePayload["error"].(map[string]any); nested["code"] != "cursor_stale" {
		t.Fatalf("stale cursor payload = %#v", stalePayload)
	}
}

func TestDefaultMCPSizeBudgets(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan(strings.Repeat("Bounded plan ", 80), "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	spec := BulkImportSpec{Tasks: make([]BulkImportTaskSpec, 0, 200)}
	for i := range 200 {
		description := strings.Repeat(fmt.Sprintf("description-%03d ", i), 500)
		if i == 0 {
			description += strings.Repeat("explicit-large-field ", 5000)
		}
		spec.Tasks = append(spec.Tasks, BulkImportTaskSpec{
			Key:         fmt.Sprintf("bounded-%03d", i),
			Name:        fmt.Sprintf("Task %03d %s", i, strings.Repeat("name ", 35)),
			Description: &description,
		})
	}
	if _, err := store.ImportTasks(plan.ID, spec, BulkImportOptions{}); err != nil {
		t.Fatalf("ImportTasks() error = %v", err)
	}
	server := NewMCPServer(root)
	for _, testCase := range []struct {
		name   string
		args   map[string]any
		budget int
	}{
		{name: "plan_stats", args: map[string]any{"plan": plan.ID}, budget: 2 * 1024},
		{name: "plan_audit", args: map[string]any{"plan": plan.ID}, budget: 2 * 1024},
		{name: "plan_ready", args: map[string]any{"plan": plan.ID}, budget: 32 * 1024},
		{
			name: "plan_get_tasks",
			args: map[string]any{
				"plan":   plan.ID,
				"status": []string{string(StatusNotStarted)},
			},
			budget: 64 * 1024,
		},
	} {
		result := server.CallTool(context.Background(), testCase.name, testCase.args)
		if result.IsError {
			t.Fatalf("%s result = %#v", testCase.name, result)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("Marshal(%s) error = %v", testCase.name, err)
		}
		if len(encoded) > testCase.budget {
			t.Errorf("%s structured bytes = %d, want <= %d", testCase.name, len(encoded), testCase.budget)
		}
		for _, content := range result.Content {
			if len([]byte(content.Text)) > 512 {
				t.Errorf("%s text bytes = %d, want <= 512", testCase.name, len([]byte(content.Text)))
			}
			if strings.Contains(content.Text, "{") || strings.Contains(content.Text, `"tasks"`) {
				t.Errorf("%s text serialized structured content: %q", testCase.name, content.Text)
			}
		}
	}
	for _, toolName := range []string{"plan_get", "plan_show"} {
		result := server.CallTool(context.Background(), toolName, map[string]any{"plan": plan.ID})
		if result.IsError {
			t.Fatalf("%s result = %#v", toolName, result)
		}
		encoded, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("Marshal(%s) error = %v", toolName, err)
		}
		if len(encoded) <= 128*1024 {
			t.Fatalf("%s structured bytes = %d, want > 128 KiB for warning coverage", toolName, len(encoded))
		}
		if len(result.Content) != 1 || !strings.Contains(result.Content[0].Text, "exceeds 128 KiB") {
			t.Fatalf("%s warning text = %#v", toolName, result.Content)
		}
		if strings.Contains(result.Content[0].Text, "{") {
			t.Fatalf("%s text duplicated structured content: %q", toolName, result.Content[0].Text)
		}
	}
	oversized := server.CallTool(context.Background(), "plan_get_tasks", map[string]any{
		"plan":   plan.ID,
		"tasks":  []string{"Task 000 " + strings.Repeat("name ", 35)},
		"fields": []string{"id", "description", "description_bytes"},
	})
	if oversized.IsError {
		t.Fatalf("plan_get_tasks(oversized) result = %#v", oversized)
	}
	oversizedBytes, _ := json.Marshal(oversized.StructuredContent)
	if len(oversizedBytes) <= 64*1024 {
		t.Fatalf("oversized structured bytes = %d, want > 64 KiB", len(oversizedBytes))
	}
	if len(oversized.Content) != 1 || !strings.Contains(oversized.Content[0].Text, "exceeds 64 KiB budget") {
		t.Fatalf("oversized warning text = %#v", oversized.Content)
	}
	metrics := server.Metrics()
	encodedMetrics, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("Marshal(metrics) error = %v", err)
	}
	if bytes.Contains(encodedMetrics, []byte("description-000")) {
		t.Fatalf("metrics captured task content: %s", encodedMetrics)
	}
	var taskMetric *OutputMetric
	var readyMetric *OutputMetric
	for i := range metrics {
		switch metrics[i].Operation {
		case "plan_get_tasks":
			if metrics[i].Paginated {
				taskMetric = &metrics[i]
			}
		case "plan_ready":
			readyMetric = &metrics[i]
		}
	}
	if taskMetric == nil || taskMetric.StructuredBytes == 0 || taskMetric.TextBytes == 0 ||
		taskMetric.Returned == 0 || taskMetric.Omitted == 0 || !taskMetric.Paginated ||
		len(taskMetric.RequestedFields) == 0 || taskMetric.PlanVersion == 0 {
		t.Fatalf("plan_get_tasks metric = %#v", taskMetric)
	}
	if readyMetric == nil || readyMetric.Returned == 0 || readyMetric.Omitted == 0 ||
		readyMetric.PlanVersion == 0 {
		t.Fatalf("plan_ready metric = %#v", readyMetric)
	}
}

func TestVersion1ManifestReadIsNonMutatingAndMutationUpgradesSafely(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "workspace")
	if err := os.MkdirAll(realRoot, defaultDirPerm); err != nil {
		t.Fatalf("MkdirAll(realRoot) error = %v", err)
	}
	aliasRoot := filepath.Join(base, "workspace-alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	realStore, err := Init(realRoot)
	if err != nil {
		t.Fatalf("Init(realRoot) error = %v", err)
	}
	plan, err := realStore.CreatePlan("Version 1", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := realStore.AddTask(plan.ID, "Legacy task", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	manifest, err := realStore.loadManifest(plan.ID)
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	manifest.Version = 1
	for i := range manifest.Tasks {
		manifest.Tasks[i].Path = realStore.taskFilePath(plan.ID, manifest.Tasks[i].ID, manifest.Tasks[i].Status)
		manifest.Tasks[i].ClaimPath = realStore.claimPath(plan.ID, manifest.Tasks[i].ID)
	}
	if err := atomicWriteJSON(realStore.planManifestPath(plan.ID), manifest); err != nil {
		t.Fatalf("write version 1 manifest error = %v", err)
	}
	before := fileChecksum(t, realStore.planManifestPath(plan.ID))

	aliasStore, err := Open(aliasRoot)
	if err != nil {
		t.Fatalf("Open(aliasRoot) error = %v", err)
	}
	if _, err := aliasStore.GetPlan(plan.ID); err != nil {
		t.Fatalf("GetPlan(version 1) error = %v", err)
	}
	if warnings := aliasStore.Warnings(); len(warnings) != 1 || !strings.Contains(warnings[0], "version 1 manifest loaded") {
		t.Fatalf("version 1 warnings = %#v", warnings)
	}
	server := NewMCPServer(aliasRoot)
	readResult := server.CallTool(context.Background(), "plan_get", map[string]any{"plan": plan.ID})
	if readResult.IsError || len(readResult.Content) != 1 ||
		!strings.Contains(readResult.Content[0].Text, "version 1 manifest loaded") {
		t.Fatalf("version 1 MCP warning result = %#v", readResult)
	}
	if afterRead := fileChecksum(t, realStore.planManifestPath(plan.ID)); afterRead != before {
		t.Fatalf("read mutated version 1 manifest: got %x want %x", afterRead, before)
	}

	description := "mutated through alias"
	if _, err := aliasStore.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{Description: &description}); err != nil {
		t.Fatalf("UpdateTask(alias) error = %v", err)
	}
	upgraded, err := realStore.loadManifest(plan.ID)
	if err != nil {
		t.Fatalf("loadManifest(upgraded) error = %v", err)
	}
	if upgraded.Version != 2 {
		t.Fatalf("manifest version = %d, want 2", upgraded.Version)
	}
	for _, summary := range upgraded.Tasks {
		if filepath.IsAbs(summary.Path) || filepath.IsAbs(summary.ClaimPath) {
			t.Fatalf("upgraded manifest retained absolute path: %#v", summary)
		}
	}
	if _, err := realStore.GetPlan(plan.ID); err != nil {
		t.Fatalf("GetPlan(upgraded) error = %v", err)
	}
}

func TestVersion1ManifestRelocatedReadUsesPlanLocalRecordsAndMutationUpgradesSafely(t *testing.T) {
	base := t.TempDir()
	sourceRoot := filepath.Join(base, "source")
	sourceStore, err := Init(sourceRoot)
	if err != nil {
		t.Fatalf("Init(sourceRoot) error = %v", err)
	}
	plan, err := sourceStore.CreatePlan("Relocated version 1", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	task, err := sourceStore.AddTask(plan.ID, "Legacy task", AddOptions{})
	if err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	manifest, err := sourceStore.loadManifest(plan.ID)
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	manifest.Version = 1
	for i := range manifest.Tasks {
		manifest.Tasks[i].Path = sourceStore.taskFilePath(plan.ID, manifest.Tasks[i].ID, manifest.Tasks[i].Status)
		manifest.Tasks[i].ClaimPath = sourceStore.claimPath(plan.ID, manifest.Tasks[i].ID)
	}
	if err := atomicWriteJSON(sourceStore.planManifestPath(plan.ID), manifest); err != nil {
		t.Fatalf("write version 1 manifest error = %v", err)
	}

	destinationRoot := filepath.Join(base, "destination")
	destinationStore, err := Init(destinationRoot)
	if err != nil {
		t.Fatalf("Init(destinationRoot) error = %v", err)
	}
	if err := os.Rename(sourceStore.planDir(plan.ID), destinationStore.planDir(plan.ID)); err != nil {
		t.Fatalf("Rename(plan directory) error = %v", err)
	}
	before := fileChecksum(t, destinationStore.planManifestPath(plan.ID))

	plans, err := destinationStore.ListPlans()
	if err != nil {
		t.Fatalf("ListPlans(relocated version 1) error = %v", err)
	}
	if len(plans) != 1 || plans[0].ID != plan.ID {
		t.Fatalf("ListPlans(relocated version 1) = %#v", plans)
	}
	if _, err := destinationStore.GetPlan(plan.ID); err != nil {
		t.Fatalf("GetPlan(relocated version 1) error = %v", err)
	}
	if afterRead := fileChecksum(t, destinationStore.planManifestPath(plan.ID)); afterRead != before {
		t.Fatalf("read mutated relocated version 1 manifest: got %x want %x", afterRead, before)
	}
	warnings := strings.Join(destinationStore.Warnings(), "\n")
	if !strings.Contains(warnings, "relocated absolute paths") {
		t.Fatalf("relocation warnings = %#v", destinationStore.Warnings())
	}

	description := "mutated after relocation"
	if _, err := destinationStore.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{Description: &description}); err != nil {
		t.Fatalf("UpdateTask(relocated version 1) error = %v", err)
	}
	upgraded, err := destinationStore.loadManifest(plan.ID)
	if err != nil {
		t.Fatalf("loadManifest(upgraded) error = %v", err)
	}
	if upgraded.Version != currentManifestVersion {
		t.Fatalf("manifest version = %d, want %d", upgraded.Version, currentManifestVersion)
	}
	for _, summary := range upgraded.Tasks {
		if filepath.IsAbs(summary.Path) || filepath.IsAbs(summary.ClaimPath) {
			t.Fatalf("upgraded manifest retained absolute path: %#v", summary)
		}
	}
}

func TestVersion1ManifestRejectsRelocatedPathWithMismatchedRecordIdentity(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	plan, err := store.CreatePlan("Unsafe relocated path", "", "")
	if err != nil {
		t.Fatalf("CreatePlan() error = %v", err)
	}
	if _, err := store.AddTask(plan.ID, "Task", AddOptions{}); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	manifest, err := store.loadManifest(plan.ID)
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	manifest.Version = 1
	manifest.Tasks[0].Path = filepath.Join(t.TempDir(), tasksDirName, "different.md")
	if err := atomicWriteJSON(store.planManifestPath(plan.ID), manifest); err != nil {
		t.Fatalf("write unsafe version 1 manifest error = %v", err)
	}
	if _, err := store.GetPlan(plan.ID); !errorHasCode(err, ErrorCodePathOutsidePlan) {
		t.Fatalf("GetPlan() error = %v, want %s", err, ErrorCodePathOutsidePlan)
	}
	manifest.Tasks[0].Path = filepath.Join(
		t.TempDir(),
		"different-plan",
		tasksDirName,
		manifest.Tasks[0].ID+".md",
	)
	if err := atomicWriteJSON(store.planManifestPath(plan.ID), manifest); err != nil {
		t.Fatalf("write wrong-plan version 1 manifest error = %v", err)
	}
	if _, err := store.GetPlan(plan.ID); !errorHasCode(err, ErrorCodePathOutsidePlan) {
		t.Fatalf("GetPlan(wrong plan) error = %v, want %s", err, ErrorCodePathOutsidePlan)
	}
}

func TestVersion2ManifestRejectsUnsafeRecordPaths(t *testing.T) {
	for _, testCase := range []struct {
		name string
		path func(root, planID, taskID string) string
	}{
		{
			name: "absolute",
			path: func(root, _, taskID string) string {
				return filepath.Join(root, tasksDirName, taskID+".md")
			},
		},
		{
			name: "traversal",
			path: func(_, _, taskID string) string {
				return filepath.Join("..", tasksDirName, taskID+".md")
			},
		},
		{
			name: "wrong task id",
			path: func(_, _, _ string) string {
				return filepath.Join(tasksDirName, "different.md")
			},
		},
		{
			name: "symlink outside",
			path: func(root, planID, taskID string) string {
				outside := filepath.Join(root, "outside.md")
				if err := os.WriteFile(outside, []byte("outside"), defaultFilePerm); err != nil {
					t.Fatalf("WriteFile(outside) error = %v", err)
				}
				link := filepath.Join(root, metaDirName, plansDirName, planID, tasksDirName, taskID+".md")
				if err := os.Remove(link); err != nil {
					t.Fatalf("Remove(record before symlink) error = %v", err)
				}
				if err := os.Symlink(outside, link); err != nil {
					t.Fatalf("Symlink(record) error = %v", err)
				}
				return filepath.Join(tasksDirName, taskID+".md")
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := Init(root)
			if err != nil {
				t.Fatalf("Init() error = %v", err)
			}
			plan, err := store.CreatePlan("Unsafe Paths", "", "")
			if err != nil {
				t.Fatalf("CreatePlan() error = %v", err)
			}
			task, err := store.AddTask(plan.ID, "Task", AddOptions{})
			if err != nil {
				t.Fatalf("AddTask() error = %v", err)
			}
			manifest, err := store.loadManifest(plan.ID)
			if err != nil {
				t.Fatalf("loadManifest() error = %v", err)
			}
			manifest.Tasks[0].Path = filepath.ToSlash(testCase.path(root, plan.ID, task.ID))
			if err := atomicWriteJSON(store.planManifestPath(plan.ID), manifest); err != nil {
				t.Fatalf("write unsafe manifest error = %v", err)
			}
			if _, err := store.GetPlan(plan.ID); err == nil {
				t.Fatal("GetPlan() unexpectedly accepted unsafe path")
			}
		})
	}
}

func manifestRecordChecksums(t *testing.T, store *Store, planID string, manifest Manifest) map[string][32]byte {
	t.Helper()
	checksums := make(map[string][32]byte, len(manifest.Tasks))
	for _, summary := range manifest.Tasks {
		checksums[summary.ID] = fileChecksum(t, resolveManifestTestPath(store, planID, summary.Path))
	}
	return checksums
}

func resolveManifestTestPath(store *Store, planID, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(store.planDir(planID), path)
}

func fileChecksum(t *testing.T, path string) [32]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return sha256.Sum256(data)
}

func assertManifestReferencesReadableRecords(t *testing.T, store *Store, planID string) {
	t.Helper()
	manifest, err := store.loadManifest(planID)
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	for _, summary := range manifest.Tasks {
		path := resolveManifestTestPath(store, planID, summary.Path)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("manifest references unreadable record %s: %v", path, err)
		}
	}
	if _, err := store.GetPlan(planID); err != nil {
		t.Fatalf("GetPlan() after failure error = %v", err)
	}
}

func assertMutationIdentity(t *testing.T, store *Store, planID string, returned *TaskRecord) {
	t.Helper()
	if returned == nil {
		t.Fatal("mutation returned nil")
	}
	loaded, err := store.GetPlan(planID)
	if err != nil {
		t.Fatalf("GetPlan() error = %v", err)
	}
	durable := loaded.GetTask(returned.ID)
	if durable == nil {
		t.Fatalf("returned task %s is absent from loaded plan", returned.ID)
	}
	if durable.Name != returned.Name || durable.Status != returned.Status || durable.Order != returned.Order {
		t.Fatalf("returned task differs from durable record: returned=%#v durable=%#v", returned, durable)
	}
}

func regressionErrorCode(err error) string {
	if err == nil {
		return ""
	}
	type coder interface {
		Code() string
	}
	if coded, ok := err.(coder); ok {
		return coded.Code()
	}
	return ""
}

func regressionMap(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return result
}

func indexOfDestination(values []string, target string) int {
	for i, value := range values {
		if value == target {
			return i
		}
	}
	return -1
}
