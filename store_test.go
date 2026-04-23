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

func TestInitAddClaimDoneFlow(t *testing.T) {
	root := t.TempDir()

	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	record, err := store.Add("Fix auth bug", AddOptions{Description: "Reproduce and patch"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if record.ID != "001-fix-auth-bug" {
		t.Fatalf("record.ID = %q", record.ID)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-plan", "tasks", "001-fix-auth-bug.md")); err != nil {
		t.Fatalf("task file missing: %v", err)
	}

	claimed, err := store.Claim(record.ID, "agent-a")
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed.ClaimedBy != "agent-a" {
		t.Fatalf("ClaimedBy = %q", claimed.ClaimedBy)
	}

	completed, err := store.Done(record.ID, "PR #47", "agent-a")
	if err != nil {
		t.Fatalf("Done() error = %v", err)
	}
	if completed.Status != StatusComplete {
		t.Fatalf("Status = %q", completed.Status)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-plan", "history", "001-fix-auth-bug.done")); err != nil {
		t.Fatalf("history file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-plan", "tasks", "001-fix-auth-bug.claim")); !os.IsNotExist(err) {
		t.Fatalf("claim file should be gone, got err=%v", err)
	}
}

func TestClaimIsExclusive(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	record, err := store.Add("First task", AddOptions{})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if _, err := store.Claim(record.ID, "agent-a"); err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	if _, err := store.Claim(record.ID, "agent-b"); err == nil {
		t.Fatal("second Claim() succeeded, want conflict")
	}
}

func TestNextRespectsDependenciesAndClaims(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	first, err := store.Add("Write tests", AddOptions{})
	if err != nil {
		t.Fatalf("Add(first) error = %v", err)
	}
	second, err := store.Add("Ship patch", AddOptions{DependsOn: []string{first.ID}})
	if err != nil {
		t.Fatalf("Add(second) error = %v", err)
	}

	next, err := store.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if next == nil || next.ID != first.ID {
		t.Fatalf("Next() = %#v, want %q", next, first.ID)
	}

	if _, err := store.Claim(first.ID, "agent-a"); err != nil {
		t.Fatalf("Claim(first) error = %v", err)
	}
	next, err = store.Next()
	if err != nil {
		t.Fatalf("Next() after claim error = %v", err)
	}
	if next != nil {
		t.Fatalf("Next() after claim = %#v, want nil", next)
	}

	if _, err := store.Done(first.ID, "", "agent-a"); err != nil {
		t.Fatalf("Done(first) error = %v", err)
	}
	next, err = store.Next()
	if err != nil {
		t.Fatalf("Next() after done error = %v", err)
	}
	if next == nil || next.ID != second.ID {
		t.Fatalf("Next() = %#v, want %q", next, second.ID)
	}
}

func TestCLIJSONList(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	code := runCLI(context.Background(), "agent-plan", []string{
		"add", "--dir", root, "--description", "Detail", "--json", "Fix auth",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCLI(add) code = %d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runCLI(context.Background(), "agent-plan", []string{
		"list", "--dir", root, "--json",
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runCLI(list) code = %d stderr=%s", code, stderr.String())
	}

	var tasks []TaskSummary
	if err := json.Unmarshal(stdout.Bytes(), &tasks); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "001-fix-auth" {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestMCPAddAndList(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)

	result := server.CallTool(context.Background(), "plan_init", map[string]any{})
	if result.IsError {
		t.Fatalf("plan_init error = %#v", result)
	}
	result = server.CallTool(context.Background(), "plan_add", map[string]any{
		"title":       "Fix auth",
		"description": "Patch login flow",
	})
	if result.IsError {
		t.Fatalf("plan_add error = %#v", result)
	}
	result = server.CallTool(context.Background(), "plan_list", map[string]any{})
	if result.IsError {
		t.Fatalf("plan_list error = %#v", result)
	}

	payload, ok := result.StructuredContent.([]TaskSummary)
	if ok {
		if len(payload) != 1 {
			t.Fatalf("len(payload) = %d", len(payload))
		}
		return
	}

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var tasks []TaskSummary
	if err := json.Unmarshal(raw, &tasks); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Fix auth" {
		t.Fatalf("tasks = %#v", tasks)
	}
}
