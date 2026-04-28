package agentplan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreHelperCoverage(t *testing.T) {
	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if _, err := Init(filepath.Join(root, metaDirName)); err != nil {
		t.Fatalf("Init(meta dir) error = %v", err)
	}
	if opened, err := Open(filepath.Join(root, "nested")); err != nil || opened.Root != root {
		t.Fatalf("Open(nested) opened=%#v err=%v", opened, err)
	}
	if opened, err := OpenOrInit(filepath.Join(root, "new-root")); err != nil || opened == nil {
		t.Fatalf("OpenOrInit() opened=%#v err=%v", opened, err)
	}

	if _, err := store.Add("", AddOptions{}); err == nil {
		t.Fatal("Add() should reject empty title")
	}
	if _, err := store.Add("Bad deps", AddOptions{DependsOn: []string{"missing"}}); err == nil {
		t.Fatal("Add() should reject missing deps")
	}

	first, err := store.Add("One", AddOptions{})
	if err != nil {
		t.Fatalf("Add(first) error = %v", err)
	}
	second, err := store.Add("Two", AddOptions{Order: intPtr(0)})
	if err != nil {
		t.Fatalf("Add(second) error = %v", err)
	}
	if second.Order != 0 {
		t.Fatalf("second.Order = %d", second.Order)
	}

	if _, err := store.List(ListOptions{Status: statusPtr(StatusComplete)}); err != nil {
		t.Fatalf("List(filter) error = %v", err)
	}

	if _, err := store.Claim("missing", ""); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Claim(missing) err = %v", err)
	}
	if _, err := store.Done("missing", "", ""); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Done(missing) err = %v", err)
	}
	if _, err := store.Block("missing", "why", ""); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Block(missing) err = %v", err)
	}
	if _, err := store.Block(first.ID, "", ""); err == nil {
		t.Fatal("Block() should reject empty reason")
	}

	if _, err := store.Claim(first.ID, "agent-a"); err != nil {
		t.Fatalf("Claim(first) error = %v", err)
	}
	if _, err := store.Done(first.ID, "", "agent-b"); err == nil {
		t.Fatal("Done() should reject mismatched claimer")
	}
	if _, err := store.Block(first.ID, "pause", "agent-b"); err == nil {
		t.Fatal("Block() should reject mismatched claimer")
	}
	blocked, err := store.Block(first.ID, "pause", "")
	if err != nil {
		t.Fatalf("Block() error = %v", err)
	}
	if blocked.Assignee != "agent-a" {
		t.Fatalf("blocked.Assignee = %q", blocked.Assignee)
	}

	overview, err := store.Overview()
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if overview.Counts[string(StatusBlocked)] == 0 {
		t.Fatalf("Overview() = %#v", overview)
	}

	if _, err := store.Done(second.ID, "done", "agent-x"); err != nil {
		t.Fatalf("Done(unclaimed) error = %v", err)
	}
	if _, err := store.Claim(second.ID, "agent-a"); err == nil {
		t.Fatal("Claim() should reject complete task")
	}

	manifest, err := store.loadManifest()
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	if next, err := store.nextFromManifest(manifest); err != nil || next != nil {
		t.Fatalf("nextFromManifest() next=%#v err=%v", next, err)
	}

	manifest.Tasks[0].ClaimPath = store.claimPath(manifest.Tasks[0].ID)
	if err := os.WriteFile(manifest.Tasks[0].ClaimPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(claim) error = %v", err)
	}
	if _, err := store.hydrateSummary(manifest.Tasks[0]); err != nil {
		t.Fatalf("hydrateSummary(invalid claim json) error = %v", err)
	}
	completeSummary := TaskSummary{ID: "complete", Status: StatusComplete}
	if hydrated, err := store.hydrateSummary(completeSummary); err != nil || hydrated.Path == "" {
		t.Fatalf("hydrateSummary(complete) hydrated=%#v err=%v", hydrated, err)
	}

	claimDir := store.claimPath("claimdir")
	if err := os.MkdirAll(claimDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(claimdir) error = %v", err)
	}
	if _, err := store.hydrateSummary(TaskSummary{ID: "claimdir"}); err == nil {
		t.Fatal("hydrateSummary() should fail on unreadable claim path")
	}

	if err := os.WriteFile(store.manifestPath(), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	if _, err := store.loadManifest(); err == nil {
		t.Fatal("loadManifest() should fail on invalid JSON")
	}

	zeroManifest := Manifest{}
	if err := store.writeManifest(zeroManifest); err != nil {
		t.Fatalf("writeManifest() error = %v", err)
	}
	loaded, err := store.loadManifest()
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}
	if loaded.Version != 1 || loaded.NextSequence != 1 {
		t.Fatalf("loaded manifest = %#v", loaded)
	}
	badClaimSummary := loaded
	badClaimSummary.Tasks = []TaskSummary{{ID: "claimdir"}}
	if next, err := store.nextFromManifest(badClaimSummary); err == nil || next != nil {
		t.Fatalf("nextFromManifest(bad) next=%#v err=%v", next, err)
	}
}

func TestStoreUtilityCoverage(t *testing.T) {
	root := t.TempDir()
	store := &Store{Root: root, MetaDir: filepath.Join(root, metaDirName)}

	if _, err := Open(root); !errors.Is(err, ErrPlanNotInitialized) {
		t.Fatalf("Open(uninitialized) err = %v", err)
	}
	if _, err := findWorkspaceRoot(filepath.Join(root, "missing")); !errors.Is(err, ErrPlanNotInitialized) {
		t.Fatalf("findWorkspaceRoot() err = %v", err)
	}

	if ids := normalizeIDs([]string{" a ", "a", "", "b"}); len(ids) != 2 {
		t.Fatalf("normalizeIDs() = %#v", ids)
	}
	summaries := []TaskSummary{{ID: "b", Order: 1, Sequence: 2}, {ID: "a", Order: 0, Sequence: 1}}
	sortSummaries(summaries)
	if summaries[0].ID != "a" || findSummaryIndex(summaries, "missing") != -1 {
		t.Fatalf("summaries = %#v", summaries)
	}
	if slugify("!!!") != "task" || !strings.Contains(preview(strings.Repeat("a", 140)), "...") || preview(" \n ") != "" {
		t.Fatal("slugify/preview unexpected result")
	}
	if slugify("Ab C-1") != "ab-c-1" {
		t.Fatalf("slugify() unexpected value")
	}

	task := TaskRecord{ID: "x", Sequence: 1, Title: "Task", Status: StatusNotStarted, Description: "body"}
	data, err := marshalTaskRecord(task)
	if err != nil {
		t.Fatalf("marshalTaskRecord() error = %v", err)
	}
	parsed, err := parseTaskRecord(data)
	if err != nil {
		t.Fatalf("parseTaskRecord() error = %v", err)
	}
	if parsed.Description != "body" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if _, err := parseTaskRecord([]byte("body")); err == nil {
		t.Fatal("parseTaskRecord() should fail without frontmatter")
	}
	if _, err := parseTaskRecord([]byte("---\na: b")); err == nil {
		t.Fatal("parseTaskRecord() should fail on unterminated frontmatter")
	}
	if _, err := parseTaskRecord([]byte("---\n:\n---\n")); err == nil {
		t.Fatal("parseTaskRecord() should fail on invalid yaml")
	}
	if marshaled, err := marshalTaskRecord(TaskRecord{}); err != nil || strings.Contains(string(marshaled), "\n\n") {
		t.Fatalf("marshalTaskRecord(empty) data=%q err=%v", string(marshaled), err)
	}

	noIDPath := filepath.Join(store.tasksDir(), "123-sample.md")
	if err := os.MkdirAll(filepath.Dir(noIDPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(noIDPath, []byte("---\nsequence: 1\ntitle: sample\nstatus: not_started\norder: 0\ncreated_at: 2026-01-01T00:00:00Z\nupdated_at: 2026-01-01T00:00:00Z\n---\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	record, err := store.readTaskRecord(noIDPath)
	if err != nil || record.ID != "123-sample" {
		t.Fatalf("readTaskRecord() record=%#v err=%v", record, err)
	}
	if _, err := store.readTaskRecord(filepath.Join(root, "missing.md")); err == nil {
		t.Fatal("readTaskRecord() should fail on missing file")
	}

	tmpFile := filepath.Join(root, "file")
	if err := os.WriteFile(tmpFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(tmpFile) error = %v", err)
	}
	badStore := &Store{Root: root, MetaDir: tmpFile}
	if err := badStore.writeManifest(Manifest{}); err == nil {
		t.Fatal("writeManifest() should fail when meta dir is a file")
	}
	if err := badStore.writeTaskRecord(filepath.Join(tmpFile, "x"), task); err == nil {
		t.Fatal("writeTaskRecord() should fail when parent is a file")
	}
	if err := writeExclusiveFile(filepath.Join(tmpFile, "x"), []byte("data"), 0o644); err == nil {
		t.Fatal("writeExclusiveFile() should fail when parent is a file")
	}
	lockPath := filepath.Join(root, "lock")
	if err := writeExclusiveFile(lockPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("writeExclusiveFile() error = %v", err)
	}
	if err := writeExclusiveFile(lockPath, []byte("data"), 0o644); !errors.Is(err, os.ErrExist) {
		t.Fatalf("writeExclusiveFile(exist) err = %v", err)
	}
	if err := atomicWriteFile(filepath.Join(tmpFile, "y"), []byte("data"), 0o644); err == nil {
		t.Fatal("atomicWriteFile() should fail when parent is a file")
	}
	if err := store.writeExclusiveJSON(filepath.Join(root, "bad.json"), func() {}); err == nil {
		t.Fatal("writeExclusiveJSON() should fail on unsupported JSON")
	}

	oldWait, oldPoll := lockWaitTimeout, lockPollInterval
	lockWaitTimeout, lockPollInterval = 10*time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		lockWaitTimeout, lockPollInterval = oldWait, oldPoll
	})
	if err := os.MkdirAll(store.MetaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(meta) error = %v", err)
	}
	if err := os.WriteFile(store.lockPath(), []byte("held"), 0o644); err != nil {
		t.Fatalf("WriteFile(lock) error = %v", err)
	}
	if err := store.withManifestLock(func() error { return nil }); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("withManifestLock() err = %v", err)
	}
	if err := os.Remove(store.lockPath()); err != nil {
		t.Fatalf("Remove(lock) error = %v", err)
	}
	if err := store.withManifestLock(func() error { return nil }); err != nil {
		t.Fatalf("withManifestLock() error = %v", err)
	}
	if err := store.withManifestLock(func() error { return errors.New("boom") }); err == nil {
		t.Fatal("withManifestLock() should return function error")
	}

	if got := defaultActor(); got == "" {
		t.Fatal("defaultActor() returned empty string")
	}
	t.Setenv("AGENT_TODO_ASSIGNEE", "todo-assignee")
	if got := defaultActor(); got != "todo-assignee" {
		t.Fatalf("defaultActor(AGENT_TODO_ASSIGNEE) = %q", got)
	}
	t.Setenv("AGENT_TODO_ASSIGNEE", "")
	t.Setenv("AGENT_NAME", "agent-name")
	if got := defaultActor(); got != "agent-name" {
		t.Fatalf("defaultActor(AGENT_NAME) = %q", got)
	}
	t.Setenv("AGENT_NAME", "")
	t.Setenv("AGENT_DASHBOARD_SESSION_ID", "session-id")
	if got := defaultActor(); got != "session-id" {
		t.Fatalf("defaultActor(AGENT_DASHBOARD_SESSION_ID) = %q", got)
	}
	t.Setenv("AGENT_DASHBOARD_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "thread-id")
	if got := defaultActor(); got != "thread-id" {
		t.Fatalf("defaultActor(CODEX_THREAD_ID) = %q", got)
	}
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("USER", "user")
	t.Setenv("HOSTNAME", "host")
	if got := defaultActor(); got != "user@host" {
		t.Fatalf("defaultActor(USER+HOSTNAME) = %q", got)
	}
	t.Setenv("HOSTNAME", "")
	if got := defaultActor(); got != "user" {
		t.Fatalf("defaultActor(USER) = %q", got)
	}
	t.Setenv("USER", "")
	t.Setenv("HOSTNAME", "host")
	if got := defaultActor(); got != "host" {
		t.Fatalf("defaultActor(HOSTNAME) = %q", got)
	}
	t.Setenv("HOSTNAME", "")
	if got := defaultActor(); !strings.HasPrefix(got, "pid-") {
		t.Fatalf("defaultActor(pid) = %q", got)
	}
	if normalized, err := normalizeRoot(filepath.Join(root, metaDirName)); err != nil || normalized != root {
		t.Fatalf("normalizeRoot(meta) normalized=%q err=%v", normalized, err)
	}
}

func intPtr(v int) *int          { return &v }
func statusPtr(v Status) *Status { return &v }
