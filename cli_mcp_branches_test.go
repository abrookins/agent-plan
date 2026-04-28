package agentplan

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCLICommandBranches(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	fileRoot := filepath.Join(root, "root-file")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(fileRoot) error = %v", err)
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := runCLI(context.Background(), "agent-plan", []string{"mcp"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("runCLI(mcp) code = %d stderr=%s", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runInit("agent-plan", &stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runInit(--bad) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runInit("agent-plan", &stdout, &stderr, []string{"--dir", fileRoot}); code != 1 {
		t.Fatalf("runInit(fileRoot) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runInit("agent-plan", &stdout, &stderr, []string{"--dir", child, "--json"}); code != 0 {
		t.Fatalf("runInit(--json) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"meta_dir"`) {
		t.Fatalf("runInit json stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runAdd("agent-plan", &stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runAdd(--bad) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAdd("agent-plan", &stdout, &stderr, []string{"--dir", fileRoot, "Task"}); code != 1 {
		t.Fatalf("runAdd(fileRoot) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAdd("agent-plan", &stdout, &stderr, []string{"--dir", root, "--order", "7", "Task one"}); code != 0 {
		t.Fatalf("runAdd(order) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "added 001-task-one") {
		t.Fatalf("runAdd stdout = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAdd("agent-plan", &stdout, &stderr, []string{"--dir", root, "--json", "Task two"}); code != 0 {
		t.Fatalf("runAdd(--json) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"id": "002-task-two"`) {
		t.Fatalf("runAdd json stdout = %q", stdout.String())
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runList(&stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runList(--bad) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runList(&stdout, &stderr, []string{"--dir", root, "--status", string(StatusNotStarted), "--json"}); code != 0 {
		t.Fatalf("runList(--json) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "002-task-two") {
		t.Fatalf("runList json stdout = %q", stdout.String())
	}
	if err := os.MkdirAll(store.claimPath("002-task-two"), 0o755); err != nil {
		t.Fatalf("MkdirAll(claim dir) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runList(&stdout, &stderr, []string{"--dir", root}); code != 1 {
		t.Fatalf("runList(claimdir) code = %d", code)
	}
	if err := os.RemoveAll(store.claimPath("002-task-two")); err != nil {
		t.Fatalf("RemoveAll(claim dir) error = %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runNext(&stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runNext(--bad) code = %d", code)
	}
	emptyRoot := t.TempDir()
	if _, err := Init(emptyRoot); err != nil {
		t.Fatalf("Init(emptyRoot) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runNext(&stdout, &stderr, []string{"--dir", emptyRoot}); code != 0 {
		t.Fatalf("runNext(empty) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no ready task") {
		t.Fatalf("runNext empty stdout = %q", stdout.String())
	}
	if err := os.WriteFile(store.manifestPath(), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runNext(&stdout, &stderr, []string{"--dir", root}); code != 1 {
		t.Fatalf("runNext(bad manifest) code = %d", code)
	}
	claimRoot := t.TempDir()
	claimStore, err := Init(claimRoot)
	if err != nil {
		t.Fatalf("Init(claimRoot) error = %v", err)
	}
	if _, err := claimStore.Add("Claim task", AddOptions{}); err != nil {
		t.Fatalf("Add(claim task) error = %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runClaim("agent-plan", &stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runClaim(--bad) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runClaim("agent-plan", &stdout, &stderr, []string{"--dir", claimRoot, "--json", "001-claim-task"}); code != 0 {
		t.Fatalf("runClaim(--json) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"claimed_by"`) {
		t.Fatalf("runClaim json stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runDone("agent-plan", &stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runDone(--bad) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runDone("agent-plan", &stdout, &stderr, []string{"--dir", claimRoot, "--by", "other", "001-claim-task"}); code != 1 {
		t.Fatalf("runDone(mismatch) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runDone("agent-plan", &stdout, &stderr, []string{"--dir", claimRoot, "--json", "001-claim-task"}); code != 0 {
		t.Fatalf("runDone(--json) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "complete"`) {
		t.Fatalf("runDone json stdout = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := runBlock("agent-plan", &stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runBlock(--bad) code = %d", code)
	}
	blockRoot := t.TempDir()
	blockStore, err := Init(blockRoot)
	if err != nil {
		t.Fatalf("Init(blockRoot) error = %v", err)
	}
	blockRecord, err := blockStore.Add("Block me", AddOptions{})
	if err != nil {
		t.Fatalf("Add(block) error = %v", err)
	}
	if _, err := blockStore.Claim(blockRecord.ID, "agent-a"); err != nil {
		t.Fatalf("Claim(block) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runBlock("agent-plan", &stdout, &stderr, []string{"--dir", blockRoot, "--by", "other", "--reason", "wait", blockRecord.ID}); code != 1 {
		t.Fatalf("runBlock(mismatch) code = %d", code)
	}

	showRoot := t.TempDir()
	showStore, err := Init(showRoot)
	if err != nil {
		t.Fatalf("Init(showRoot) error = %v", err)
	}
	if _, err := showStore.Add("Show next", AddOptions{}); err != nil {
		t.Fatalf("Add(show) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runPlanShow("agent-plan", &stdout, &stderr, []string{"--bad"}); code != 1 {
		t.Fatalf("runPlanShow(--bad) code = %d", code)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runPlanShow("agent-plan", &stdout, &stderr, []string{"--dir", showRoot}); code != 0 {
		t.Fatalf("runPlanShow(code) = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "next: 001-show-next") {
		t.Fatalf("runPlanShow stdout = %q", stdout.String())
	}
	if err := os.WriteFile(showStore.manifestPath(), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(show manifest) error = %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runPlanShow("agent-plan", &stdout, &stderr, []string{"--dir", showRoot}); code != 1 {
		t.Fatalf("runPlanShow(bad manifest) code = %d", code)
	}
}

func TestMCPCallToolBranches(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)

	fileRoot := filepath.Join(root, "root-file")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(fileRoot) error = %v", err)
	}

	if result := server.CallTool(context.Background(), "plan_init", map[string]any{"root": fileRoot}); !result.IsError {
		t.Fatal("plan_init(fileRoot) should fail")
	}

	if result := server.CallTool(context.Background(), "plan_add", map[string]any{"root": fileRoot, "title": "Task"}); !result.IsError {
		t.Fatal("plan_add(fileRoot) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_add", map[string]any{"root": root, "title": "Bad deps", "depends_on": []any{"missing"}}); !result.IsError {
		t.Fatal("plan_add(bad deps) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_list", map[string]any{"root": t.TempDir()}); !result.IsError {
		t.Fatal("plan_list(uninitialized) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_next", map[string]any{"root": t.TempDir()}); !result.IsError {
		t.Fatal("plan_next(uninitialized) should fail")
	}
	emptyRoot := t.TempDir()
	if _, err := Init(emptyRoot); err != nil {
		t.Fatalf("Init(emptyRoot) error = %v", err)
	}
	if result := NewMCPServer(emptyRoot).CallTool(context.Background(), "plan_next", map[string]any{}); result.IsError {
		t.Fatalf("plan_next(empty) should succeed: %#v", result)
	}
	if result := server.CallTool(context.Background(), "plan_claim", map[string]any{}); !result.IsError {
		t.Fatal("plan_claim(missing id) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_claim", map[string]any{"root": t.TempDir(), "id": "missing"}); !result.IsError {
		t.Fatal("plan_claim(uninitialized) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_done", map[string]any{}); !result.IsError {
		t.Fatal("plan_done(missing id) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_done", map[string]any{"root": t.TempDir(), "id": "missing"}); !result.IsError {
		t.Fatal("plan_done(uninitialized) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_block", map[string]any{}); !result.IsError {
		t.Fatal("plan_block(missing id) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_block", map[string]any{"id": "x"}); !result.IsError {
		t.Fatal("plan_block(missing reason) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_block", map[string]any{"root": t.TempDir(), "id": "missing", "reason": "wait"}); !result.IsError {
		t.Fatal("plan_block(uninitialized) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_show", map[string]any{"root": t.TempDir()}); !result.IsError {
		t.Fatal("plan_show(uninitialized) should fail")
	}

	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	record, err := store.Add("Task", AddOptions{})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := os.MkdirAll(store.claimPath(record.ID), 0o755); err != nil {
		t.Fatalf("MkdirAll(claim dir) error = %v", err)
	}
	if result := server.CallTool(context.Background(), "plan_list", map[string]any{"root": root}); !result.IsError {
		t.Fatal("plan_list(claim dir) should fail")
	}
	if err := os.RemoveAll(store.claimPath(record.ID)); err != nil {
		t.Fatalf("RemoveAll(claim dir) error = %v", err)
	}
	if err := os.MkdirAll(store.claimPath(record.ID), 0o755); err != nil {
		t.Fatalf("MkdirAll(claim dir) error = %v", err)
	}
	if result := server.CallTool(context.Background(), "plan_next", map[string]any{"root": root}); !result.IsError {
		t.Fatal("plan_next(claim dir) should fail")
	}
	if err := os.RemoveAll(store.claimPath(record.ID)); err != nil {
		t.Fatalf("RemoveAll(claim dir) error = %v", err)
	}

	if result := server.CallTool(context.Background(), "plan_claim", map[string]any{"root": root, "id": "missing"}); !result.IsError {
		t.Fatal("plan_claim(missing task) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_done", map[string]any{"root": root, "id": "missing"}); !result.IsError {
		t.Fatal("plan_done(missing task) should fail")
	}
	if result := server.CallTool(context.Background(), "plan_block", map[string]any{"root": root, "id": "missing", "reason": "wait"}); !result.IsError {
		t.Fatal("plan_block(missing task) should fail")
	}

	if _, err := store.Claim(record.ID, "agent-a"); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if result := server.CallTool(context.Background(), "plan_done", map[string]any{"root": root, "id": record.ID, "by": "other"}); !result.IsError {
		t.Fatal("plan_done(mismatch) should fail")
	}
	blockRoot := t.TempDir()
	blockStore, err := Init(blockRoot)
	if err != nil {
		t.Fatalf("Init(blockRoot) error = %v", err)
	}
	blockRecord, err := blockStore.Add("Blocked", AddOptions{})
	if err != nil {
		t.Fatalf("Add(block) error = %v", err)
	}
	if _, err := blockStore.Claim(blockRecord.ID, "agent-a"); err != nil {
		t.Fatalf("Claim(block) error = %v", err)
	}
	if result := NewMCPServer(blockRoot).CallTool(context.Background(), "plan_block", map[string]any{"id": blockRecord.ID, "reason": "wait", "by": "other"}); !result.IsError {
		t.Fatal("plan_block(mismatch) should fail")
	}
	successRoot := t.TempDir()
	successStore, err := Init(successRoot)
	if err != nil {
		t.Fatalf("Init(successRoot) error = %v", err)
	}
	successRecord, err := successStore.Add("Block success", AddOptions{})
	if err != nil {
		t.Fatalf("Add(success) error = %v", err)
	}
	if result := NewMCPServer(successRoot).CallTool(context.Background(), "plan_block", map[string]any{"id": successRecord.ID, "reason": "wait"}); result.IsError {
		t.Fatalf("plan_block(success) should succeed: %#v", result)
	}

	showRoot := t.TempDir()
	showStore, err := Init(showRoot)
	if err != nil {
		t.Fatalf("Init(showRoot) error = %v", err)
	}
	showRecord, err := showStore.Add("Show", AddOptions{})
	if err != nil {
		t.Fatalf("Add(show) error = %v", err)
	}
	if err := os.MkdirAll(showStore.claimPath(showRecord.ID), 0o755); err != nil {
		t.Fatalf("MkdirAll(show claim dir) error = %v", err)
	}
	if result := NewMCPServer(showRoot).CallTool(context.Background(), "plan_show", map[string]any{}); !result.IsError {
		t.Fatal("plan_show(claim dir) should fail")
	}
}

func TestRunMCPViaCLIProducesProtocolFrame(t *testing.T) {
	root := t.TempDir()
	request := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	input := "Content-Length: " + strconv.Itoa(len(request)) + "\r\n\r\n" + request
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	if code := runCLI(context.Background(), "agent-plan", []string{"mcp", root}, strings.NewReader(input), &stdout, &stderr); code != 0 {
		t.Fatalf("runCLI(mcp) code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "serverInfo") {
		t.Fatalf("runCLI(mcp) stdout = %q", stdout.String())
	}
}

func TestCLIJSONShapes(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	if code := runAdd("agent-plan", &stdout, &stderr, []string{"--dir", root, "--json", "Shape"}); code != 0 {
		t.Fatalf("runAdd(--json) code = %d stderr=%s", code, stderr.String())
	}
	var record TaskRecord
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if record.ID == "" {
		t.Fatalf("record = %#v", record)
	}
}
