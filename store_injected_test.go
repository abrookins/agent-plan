package agentplan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type stubExclusiveFile struct {
	writeErr error
	syncErr  error
	closeErr error
}

func (f *stubExclusiveFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *stubExclusiveFile) Sync() error  { return f.syncErr }
func (f *stubExclusiveFile) Close() error { return f.closeErr }

type stubTempFile struct {
	name     string
	writeErr error
	chmodErr error
	syncErr  error
	closeErr error
}

func (f *stubTempFile) Name() string { return f.name }

func (f *stubTempFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *stubTempFile) Chmod(os.FileMode) error { return f.chmodErr }
func (f *stubTempFile) Sync() error             { return f.syncErr }
func (f *stubTempFile) Close() error            { return f.closeErr }

func restoreStoreFuncs(t *testing.T) {
	t.Helper()
	oldJSONMarshalIndent := jsonMarshalIndent
	oldYAMLMarshalFunc := yamlMarshalFunc
	oldYAMLUnmarshalFunc := yamlUnmarshalFunc
	oldMkdirAllFunc := mkdirAllFunc
	oldStatFunc := statFunc
	oldReadFileFunc := readFileFunc
	oldRemoveFileFunc := removeFileFunc
	oldRenameFunc := renameFunc
	oldGetwdFunc := getwdFunc
	oldAbsFunc := absFunc
	oldOpenExclusiveFileFunc := openExclusiveFileFunc
	oldCreateTempFileFunc := createTempFileFunc
	t.Cleanup(func() {
		jsonMarshalIndent = oldJSONMarshalIndent
		yamlMarshalFunc = oldYAMLMarshalFunc
		yamlUnmarshalFunc = oldYAMLUnmarshalFunc
		mkdirAllFunc = oldMkdirAllFunc
		statFunc = oldStatFunc
		readFileFunc = oldReadFileFunc
		removeFileFunc = oldRemoveFileFunc
		renameFunc = oldRenameFunc
		getwdFunc = oldGetwdFunc
		absFunc = oldAbsFunc
		openExclusiveFileFunc = oldOpenExclusiveFileFunc
		createTempFileFunc = oldCreateTempFileFunc
	})
}

func resetStoreFuncs() {
	jsonMarshalIndent = json.MarshalIndent
	yamlMarshalFunc = yaml.Marshal
	yamlUnmarshalFunc = yaml.Unmarshal
	mkdirAllFunc = os.MkdirAll
	statFunc = os.Stat
	readFileFunc = os.ReadFile
	removeFileFunc = os.Remove
	renameFunc = os.Rename
	getwdFunc = os.Getwd
	absFunc = filepath.Abs
	openExclusiveFileFunc = func(path string, perm os.FileMode) (exclusiveFile, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	}
	createTempFileFunc = func(dir, pattern string) (tempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
}

func TestStoreInjectedCoreErrorPaths(t *testing.T) {
	restoreStoreFuncs(t)

	jsonMarshalIndent = func(any, string, string) ([]byte, error) {
		return nil, errors.New("json boom")
	}
	if _, err := Init(t.TempDir()); err == nil || !strings.Contains(err.Error(), "json boom") {
		t.Fatalf("Init(json error) err = %v", err)
	}
	resetStoreFuncs()

	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	jsonMarshalIndent = func(any, string, string) ([]byte, error) {
		return nil, errors.New("json boom")
	}
	if err := store.writeManifest(Manifest{}); err == nil || !strings.Contains(err.Error(), "json boom") {
		t.Fatalf("writeManifest() err = %v", err)
	}

	resetStoreFuncs()
	statFunc = func(path string) (os.FileInfo, error) {
		if strings.HasSuffix(path, manifestFileName) {
			return nil, errors.New("stat boom")
		}
		return os.Stat(path)
	}
	if _, err := Init(t.TempDir()); err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("Init(stat error) err = %v", err)
	}
}

func TestStoreInjectedHelpersAndOperations(t *testing.T) {
	restoreStoreFuncs(t)

	root := t.TempDir()
	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	jsonMarshalIndent = func(any, string, string) ([]byte, error) {
		return nil, errors.New("json boom")
	}
	if _, err := store.Add("manifest write fails", AddOptions{}); err == nil || !strings.Contains(err.Error(), "json boom") {
		t.Fatalf("Add(json error) err = %v", err)
	}

	resetStoreFuncs()
	root = t.TempDir()
	store, err = Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	yamlMarshalFunc = func(any) ([]byte, error) {
		return nil, errors.New("yaml boom")
	}
	if _, err := store.Add("task write fails", AddOptions{}); err == nil || !strings.Contains(err.Error(), "yaml boom") {
		t.Fatalf("Add(yaml error) err = %v", err)
	}

	resetStoreFuncs()
	root = t.TempDir()
	store, err = Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := store.Add("task", AddOptions{}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := os.WriteFile(store.manifestPath(), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	if _, err := store.Add("bad manifest add", AddOptions{}); err == nil {
		t.Fatal("Add() should fail on invalid manifest")
	}

	resetStoreFuncs()
	root = t.TempDir()
	store, err = Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := store.Add("task", AddOptions{}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := os.WriteFile(store.manifestPath(), []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	if _, err := store.List(ListOptions{}); err == nil {
		t.Fatal("List() should fail on invalid manifest")
	}
	if _, err := store.Next(); err == nil {
		t.Fatal("Next() should fail on invalid manifest")
	}
	if _, err := store.Claim("001-task", "agent"); err == nil {
		t.Fatal("Claim() should fail on invalid manifest")
	}
	if _, err := store.Done("001-task", "", "agent"); err == nil {
		t.Fatal("Done() should fail on invalid manifest")
	}
	if _, err := store.Block("001-task", "wait", "agent"); err == nil {
		t.Fatal("Block() should fail on invalid manifest")
	}
	if _, err := store.Overview(); err == nil {
		t.Fatal("Overview() should fail on invalid manifest")
	}
}

func TestStoreFilesystemAndInjectedBranches(t *testing.T) {
	restoreStoreFuncs(t)

	root := t.TempDir()
	if manifestOnly, err := normalizeRoot(filepath.Join(root, metaDirName)); err != nil || manifestOnly != root {
		t.Fatalf("normalizeRoot(meta) got %q err=%v", manifestOnly, err)
	}

	fileRoot := filepath.Join(root, "workspace-file")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(fileRoot) error = %v", err)
	}
	if _, err := Init(fileRoot); err == nil {
		t.Fatal("Init() should fail when root is a file")
	}

	historyRoot := filepath.Join(root, "history-root")
	if err := os.MkdirAll(filepath.Join(historyRoot, metaDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(historyRoot, metaDirName, historyDirName), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(history) error = %v", err)
	}
	if _, err := Init(historyRoot); err == nil {
		t.Fatal("Init() should fail when history path is a file")
	}

	manifestDirRoot := filepath.Join(root, "manifest-dir-root")
	if err := os.MkdirAll(filepath.Join(manifestDirRoot, metaDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta) error = %v", err)
	}
	if err := os.Mkdir(filepath.Join(manifestDirRoot, metaDirName, manifestFileName), 0o755); err != nil {
		t.Fatalf("Mkdir(manifest dir) error = %v", err)
	}
	if _, err := Open(manifestDirRoot); err != nil {
		t.Fatalf("Open(manifest dir) error = %v", err)
	}

	missingManifestRoot := filepath.Join(root, "missing-manifest")
	if err := os.MkdirAll(filepath.Join(missingManifestRoot, metaDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta) error = %v", err)
	}
	if _, err := Open(missingManifestRoot); !errors.Is(err, ErrPlanNotInitialized) {
		t.Fatalf("Open(missing manifest) err = %v", err)
	}

	statFunc = func(path string) (os.FileInfo, error) {
		if path == filepath.Join(root, metaDirName, manifestFileName) {
			return nil, errors.New("stat boom")
		}
		return os.Stat(path)
	}
	if err := os.MkdirAll(filepath.Join(root, metaDirName), 0o755); err != nil {
		t.Fatalf("MkdirAll(meta) error = %v", err)
	}
	if _, err := Open(root); err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("Open(stat error) err = %v", err)
	}

	getwdFunc = func() (string, error) { return "", errors.New("getwd boom") }
	if _, err := Init(""); err == nil || !strings.Contains(err.Error(), "getwd boom") {
		t.Fatalf("Init(getwd error) err = %v", err)
	}

	resetStoreFuncs()
	blankRoot, err := normalizeRoot("")
	if err != nil || blankRoot == "" {
		t.Fatalf("normalizeRoot(blank) root=%q err=%v", blankRoot, err)
	}
	absFunc = func(string) (string, error) { return "", errors.New("abs boom") }
	if _, err := normalizeRoot("relative"); err == nil || !strings.Contains(err.Error(), "abs boom") {
		t.Fatalf("normalizeRoot(abs error) err = %v", err)
	}
	if _, err := findWorkspaceRoot("relative"); err == nil || !strings.Contains(err.Error(), "abs boom") {
		t.Fatalf("findWorkspaceRoot(abs error) err = %v", err)
	}
	if _, err := OpenOrInit("relative"); err == nil || !strings.Contains(err.Error(), "abs boom") {
		t.Fatalf("OpenOrInit(abs error) err = %v", err)
	}

	resetStoreFuncs()
	store := &Store{Root: root, MetaDir: filepath.Join(root, metaDirName)}
	if _, err := store.loadManifest(); !errors.Is(err, ErrPlanNotInitialized) {
		t.Fatalf("loadManifest(not initialized) err = %v", err)
	}
	readFileFunc = func(string) ([]byte, error) { return nil, errors.New("read boom") }
	if _, err := store.loadManifest(); err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("loadManifest(read error) err = %v", err)
	}

	resetStoreFuncs()
	store = &Store{Root: root, MetaDir: filepath.Join(root, metaDirName)}
	invalidTaskPath := filepath.Join(root, "invalid.md")
	if err := os.WriteFile(invalidTaskPath, []byte("not yaml"), 0o644); err != nil {
		t.Fatalf("WriteFile(task) error = %v", err)
	}
	if _, err := store.readTaskRecord(invalidTaskPath); err == nil {
		t.Fatal("readTaskRecord() should fail on invalid task file")
	}

	yamlMarshalFunc = func(any) ([]byte, error) { return nil, errors.New("yaml boom") }
	if _, err := marshalTaskRecord(TaskRecord{}); err == nil || !strings.Contains(err.Error(), "yaml boom") {
		t.Fatalf("marshalTaskRecord() err = %v", err)
	}
	if err := store.writeTaskRecord(filepath.Join(root, "task.md"), TaskRecord{}); err == nil || !strings.Contains(err.Error(), "yaml boom") {
		t.Fatalf("writeTaskRecord() err = %v", err)
	}

	metaAsFile := filepath.Join(root, "meta-file")
	if err := os.WriteFile(metaAsFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile(meta-file) error = %v", err)
	}
	if err := (&Store{Root: root, MetaDir: metaAsFile}).withManifestLock(func() error { return nil }); err == nil {
		t.Fatal("withManifestLock() should fail when meta dir is a file")
	}

	openExclusiveFileFunc = func(string, os.FileMode) (exclusiveFile, error) {
		return nil, errors.New("open boom")
	}
	if err := writeExclusiveFile(filepath.Join(root, "claim"), []byte("x"), 0o644); err == nil || !strings.Contains(err.Error(), "open boom") {
		t.Fatalf("writeExclusiveFile(open error) err = %v", err)
	}
	if err := (&Store{Root: root, MetaDir: filepath.Join(root, metaDirName)}).withManifestLock(func() error { return nil }); err == nil || !strings.Contains(err.Error(), "open boom") {
		t.Fatalf("withManifestLock(open error) err = %v", err)
	}

	openExclusiveFileFunc = func(string, os.FileMode) (exclusiveFile, error) {
		return &stubExclusiveFile{writeErr: errors.New("write boom")}, nil
	}
	if err := writeExclusiveFile(filepath.Join(root, "claim-write"), []byte("x"), 0o644); err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("writeExclusiveFile(write error) err = %v", err)
	}

	createTempFileFunc = func(string, string) (tempFile, error) {
		return nil, errors.New("temp boom")
	}
	if err := atomicWriteFile(filepath.Join(root, "atomic"), []byte("x"), 0o644); err == nil || !strings.Contains(err.Error(), "temp boom") {
		t.Fatalf("atomicWriteFile(temp error) err = %v", err)
	}

	createTempFileFunc = func(dir, pattern string) (tempFile, error) {
		return &stubTempFile{name: filepath.Join(dir, "tmp-write"), writeErr: errors.New("write boom")}, nil
	}
	if err := atomicWriteFile(filepath.Join(root, "atomic-write"), []byte("x"), 0o644); err == nil || !strings.Contains(err.Error(), "write boom") {
		t.Fatalf("atomicWriteFile(write error) err = %v", err)
	}

	createTempFileFunc = func(dir, pattern string) (tempFile, error) {
		return &stubTempFile{name: filepath.Join(dir, "tmp-chmod"), chmodErr: errors.New("chmod boom")}, nil
	}
	if err := atomicWriteFile(filepath.Join(root, "atomic-chmod"), []byte("x"), 0o644); err == nil || !strings.Contains(err.Error(), "chmod boom") {
		t.Fatalf("atomicWriteFile(chmod error) err = %v", err)
	}

	createTempFileFunc = func(dir, pattern string) (tempFile, error) {
		return &stubTempFile{name: filepath.Join(dir, "tmp-sync"), syncErr: errors.New("sync boom")}, nil
	}
	if err := atomicWriteFile(filepath.Join(root, "atomic-sync"), []byte("x"), 0o644); err == nil || !strings.Contains(err.Error(), "sync boom") {
		t.Fatalf("atomicWriteFile(sync error) err = %v", err)
	}

	createTempFileFunc = func(dir, pattern string) (tempFile, error) {
		return &stubTempFile{name: filepath.Join(dir, "tmp-close"), closeErr: errors.New("close boom")}, nil
	}
	if err := atomicWriteFile(filepath.Join(root, "atomic-close"), []byte("x"), 0o644); err == nil || !strings.Contains(err.Error(), "close boom") {
		t.Fatalf("atomicWriteFile(close error) err = %v", err)
	}
}

func TestStoreOperationErrorBranches(t *testing.T) {
	restoreStoreFuncs(t)

	makeStoreWithTask := func(t *testing.T) (*Store, *TaskRecord) {
		t.Helper()
		root := t.TempDir()
		store, err := Init(root)
		if err != nil {
			t.Fatalf("Init() error = %v", err)
		}
		record, err := store.Add("Task", AddOptions{})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}
		return store, record
	}

	t.Run("claim branches", func(t *testing.T) {
		resetStoreFuncs()
		store, record := makeStoreWithTask(t)

		if err := os.MkdirAll(store.claimPath(record.ID), 0o755); err != nil {
			t.Fatalf("MkdirAll(claim dir) error = %v", err)
		}
		if _, err := store.Claim(record.ID, "agent"); err == nil {
			t.Fatal("Claim() should fail when claim path is unreadable")
		}

		store, record = makeStoreWithTask(t)
		openExclusiveFileFunc = func(path string, perm os.FileMode) (exclusiveFile, error) {
			if path == store.lockPath() {
				return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
			}
			return nil, errors.New("claim open boom")
		}
		if _, err := store.Claim(record.ID, "agent"); err == nil || !strings.Contains(err.Error(), "claim open boom") {
			t.Fatalf("Claim(open error) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if err := os.WriteFile(store.claimPath(record.ID), []byte("{"), 0o644); err != nil {
			t.Fatalf("WriteFile(claim) error = %v", err)
		}
		if _, err := store.Claim(record.ID, "agent"); !errors.Is(err, ErrTaskAlreadyClaimed) {
			t.Fatalf("Claim(existing invalid claim) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if err := os.Remove(record.Path); err != nil {
			t.Fatalf("Remove(task) error = %v", err)
		}
		if _, err := store.Claim(record.ID, "agent"); err == nil {
			t.Fatal("Claim() should fail when task file is missing")
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		yamlMarshalFunc = func(any) ([]byte, error) { return nil, errors.New("yaml boom") }
		if _, err := store.Claim(record.ID, "agent"); err == nil || !strings.Contains(err.Error(), "yaml boom") {
			t.Fatalf("Claim(yaml error) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		jsonMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("json boom") }
		if _, err := store.Claim(record.ID, "agent"); err == nil || !strings.Contains(err.Error(), "json boom") {
			t.Fatalf("Claim(json error) err = %v", err)
		}
	})

	t.Run("done branches", func(t *testing.T) {
		resetStoreFuncs()
		store, record := makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if err := os.Remove(store.claimPath(record.ID)); err != nil {
			t.Fatalf("Remove(claim) error = %v", err)
		}
		if err := os.MkdirAll(store.claimPath(record.ID), 0o755); err != nil {
			t.Fatalf("MkdirAll(claim dir) error = %v", err)
		}
		if _, err := store.Done(record.ID, "", ""); err == nil {
			t.Fatal("Done() should fail when claim path is unreadable")
		}

		store, record = makeStoreWithTask(t)
		if err := os.Remove(record.Path); err != nil {
			t.Fatalf("Remove(task) error = %v", err)
		}
		if _, err := store.Done(record.ID, "", ""); err == nil {
			t.Fatal("Done() should fail when task file is missing")
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		yamlMarshalFunc = func(any) ([]byte, error) { return nil, errors.New("yaml boom") }
		if _, err := store.Done(record.ID, "", ""); err == nil || !strings.Contains(err.Error(), "yaml boom") {
			t.Fatalf("Done(yaml error) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		removeFileFunc = func(path string) error {
			if path == record.Path {
				return errors.New("remove active boom")
			}
			return os.Remove(path)
		}
		if _, err := store.Done(record.ID, "", ""); err == nil || !strings.Contains(err.Error(), "remove active boom") {
			t.Fatalf("Done(remove active) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		removeFileFunc = func(path string) error {
			if path == store.claimPath(record.ID) {
				return errors.New("remove claim boom")
			}
			return os.Remove(path)
		}
		if _, err := store.Done(record.ID, "", ""); err == nil || !strings.Contains(err.Error(), "remove claim boom") {
			t.Fatalf("Done(remove claim) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		jsonMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("json boom") }
		if _, err := store.Done(record.ID, "", ""); err == nil || !strings.Contains(err.Error(), "json boom") {
			t.Fatalf("Done(json error) err = %v", err)
		}
	})

	t.Run("block branches", func(t *testing.T) {
		resetStoreFuncs()
		store, record := makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		if err := os.Remove(store.claimPath(record.ID)); err != nil {
			t.Fatalf("Remove(claim) error = %v", err)
		}
		if err := os.MkdirAll(store.claimPath(record.ID), 0o755); err != nil {
			t.Fatalf("MkdirAll(claim dir) error = %v", err)
		}
		if _, err := store.Block(record.ID, "wait", ""); err == nil {
			t.Fatal("Block() should fail when claim path is unreadable")
		}

		store, record = makeStoreWithTask(t)
		if err := os.Remove(record.Path); err != nil {
			t.Fatalf("Remove(task) error = %v", err)
		}
		if _, err := store.Block(record.ID, "wait", ""); err == nil {
			t.Fatal("Block() should fail when task file is missing")
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		yamlMarshalFunc = func(any) ([]byte, error) { return nil, errors.New("yaml boom") }
		if _, err := store.Block(record.ID, "wait", ""); err == nil || !strings.Contains(err.Error(), "yaml boom") {
			t.Fatalf("Block(yaml error) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		removeFileFunc = func(path string) error {
			if path == store.claimPath(record.ID) {
				return errors.New("remove claim boom")
			}
			return os.Remove(path)
		}
		if _, err := store.Block(record.ID, "wait", ""); err == nil || !strings.Contains(err.Error(), "remove claim boom") {
			t.Fatalf("Block(remove claim) err = %v", err)
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		if _, err := store.Claim(record.ID, "agent"); err != nil {
			t.Fatalf("Claim() error = %v", err)
		}
		jsonMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("json boom") }
		if _, err := store.Block(record.ID, "wait", ""); err == nil || !strings.Contains(err.Error(), "json boom") {
			t.Fatalf("Block(json error) err = %v", err)
		}
	})

	t.Run("overview next error", func(t *testing.T) {
		resetStoreFuncs()
		store, record := makeStoreWithTask(t)
		if err := os.MkdirAll(store.claimPath(record.ID), 0o755); err != nil {
			t.Fatalf("MkdirAll(claim dir) error = %v", err)
		}
		if _, err := store.Overview(); err == nil {
			t.Fatal("Overview() should fail when hydrateSummary fails")
		}

		resetStoreFuncs()
		store, record = makeStoreWithTask(t)
		readCount := 0
		readFileFunc = func(path string) ([]byte, error) {
			if path == store.claimPath(record.ID) {
				readCount++
				if readCount == 2 {
					return nil, errors.New("claim read boom")
				}
			}
			return os.ReadFile(path)
		}
		if _, err := store.Overview(); err == nil || !strings.Contains(err.Error(), "claim read boom") {
			t.Fatalf("Overview(nextFromManifest error) err = %v", err)
		}
	})
}
