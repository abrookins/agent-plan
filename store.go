package agentplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"crypto/rand"

	"gopkg.in/yaml.v3"
)

const (
	metaDirName            = ".agent-plan"
	plansDirName           = "plans"
	workspaceFileName      = "workspace.json"
	workspaceLockName      = "workspace.lock"
	planFileName           = "plan.json"
	tasksDirName           = "tasks"
	historyDirName         = "history"
	manifestFileName       = "manifest.json"
	manifestLockName       = "manifest.lock"
	defaultFilePerm        = 0o644
	defaultDirPerm         = 0o755
	currentStoreFormat     = 2
	currentManifestVersion = 2
)

var (
	lockWaitTimeout       = 5 * time.Second
	lockPollInterval      = 50 * time.Millisecond
	jsonMarshalIndent     = json.MarshalIndent
	yamlMarshalFunc       = yaml.Marshal
	yamlUnmarshalFunc     = yaml.Unmarshal
	mkdirAllFunc          = os.MkdirAll
	statFunc              = os.Stat
	readFileFunc          = os.ReadFile
	removeFileFunc        = os.Remove
	removeAllFunc         = os.RemoveAll
	renameFunc            = os.Rename
	readDirFunc           = os.ReadDir
	getwdFunc             = os.Getwd
	absFunc               = filepath.Abs
	lstatFunc             = os.Lstat
	evalSymlinksFunc      = filepath.EvalSymlinks
	openExclusiveFileFunc = func(path string, perm os.FileMode) (exclusiveFile, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	}
	createTempFileFunc = func(dir, pattern string) (tempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	ErrPlanNotInitialized = errors.New("agent plan is not initialized in this workspace")
	ErrTaskNotFound       = errors.New("task not found")
	ErrTaskAlreadyClaimed = errors.New("task is already claimed")
)

type exclusiveFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type tempFile interface {
	Name() string
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

type Store struct {
	Root     string
	MetaDir  string
	warnings []string
}

func Init(root string) (*Store, error) {
	selectedRoot, err := selectRoot(root)
	if err != nil {
		return nil, err
	}
	if err := mkdirAllFunc(selectedRoot, defaultDirPerm); err != nil {
		return nil, err
	}
	resolvedRoot, err := canonicalizePath(selectedRoot)
	if err != nil {
		return nil, err
	}
	store := &Store{
		Root:    resolvedRoot,
		MetaDir: filepath.Join(resolvedRoot, metaDirName),
	}
	if err := validateMetaDirWithinRoot(store.Root, store.MetaDir); err != nil {
		return nil, err
	}
	if err := mkdirAllFunc(store.MetaDir, defaultDirPerm); err != nil {
		return nil, err
	}
	if err := store.withWorkspaceLock(func() error {
		return store.ensureWorkspaceLocked()
	}); err != nil {
		return nil, err
	}
	return store, nil
}

func Open(root string) (*Store, error) {
	resolvedRoot, err := findWorkspaceRoot(root)
	if err != nil {
		return nil, err
	}
	store := &Store{
		Root:    resolvedRoot,
		MetaDir: filepath.Join(resolvedRoot, metaDirName),
	}
	if err := store.withWorkspaceLock(func() error {
		_, err := store.loadWorkspace()
		return err
	}); err != nil {
		return nil, err
	}
	return store, nil
}

func OpenOrInit(root string) (*Store, error) {
	store, err := Open(root)
	if err == nil {
		return store, nil
	}
	if !errors.Is(err, ErrPlanNotInitialized) {
		return nil, err
	}
	return Init(root)
}

func (s *Store) CreatePlan(name, description, specPath string) (*Plan, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("plan name is required")
	}
	var created *Plan
	err := s.withWorkspaceLock(func() error {
		if err := s.ensureWorkspaceLocked(); err != nil {
			return err
		}
		planID, err := randomID()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		plan := &Plan{
			ID:          planID,
			Name:        name,
			Description: description,
			SpecPath:    strings.TrimSpace(specPath),
			CreatedAt:   now,
			UpdatedAt:   now,
			Version:     1,
		}
		manifest := &Manifest{
			Version:      currentManifestVersion,
			PlanID:       plan.ID,
			CreatedAt:    now,
			UpdatedAt:    now,
			NextSequence: 1,
			Tasks:        []TaskSummary{},
		}
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
		}
		created = plan
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) ListPlans() ([]PlanSummary, error) {
	if _, err := s.loadWorkspace(); err != nil {
		return nil, err
	}
	entries, err := readDirFunc(s.plansDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	summaries := make([]PlanSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		planRecord, err := s.loadPlanRecord(entry.Name())
		if err != nil {
			return nil, err
		}
		manifest, err := s.loadManifest(entry.Name())
		if err != nil {
			return nil, err
		}
		complete := 0
		for _, task := range manifest.Tasks {
			if task.Status == StatusComplete {
				complete++
			}
		}
		summaries = append(summaries, PlanSummary{
			ID:            planRecord.ID,
			Name:          planRecord.Name,
			Description:   planRecord.Description,
			SpecPath:      planRecord.SpecPath,
			TaskCount:     len(manifest.Tasks),
			CompleteCount: complete,
			UpdatedAt:     planRecord.UpdatedAt,
		})
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}

func (s *Store) GetPlan(planID string) (*Plan, error) {
	plan, _, err := s.loadPlan(planID)
	return plan, err
}

func (s *Store) UpdatePlan(planID string, opts PlanUpdateOptions) (*Plan, error) {
	var updated *Plan
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		plan.ApplyPlanUpdate(opts)
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
		}
		updated = plan
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Store) DeletePlan(planID string) (bool, error) {
	removed := false
	err := s.withWorkspaceLock(func() error {
		if _, err := statFunc(s.planDir(planID)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if err := removeAllFunc(s.planDir(planID)); err != nil {
			return err
		}
		removed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

func (s *Store) AddTask(planID, name string, opts AddOptions) (*TaskRecord, error) {
	var created *TaskRecord
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		record, err := plan.AddTask(name, opts.Description, opts.ParentID, opts.Order, manifest.NextSequence)
		if err != nil {
			return err
		}
		createdID := record.ID
		deps, err := plan.ValidateDependencyIDs(opts.DependsOn, createdID)
		if err != nil {
			return err
		}
		task := plan.GetTask(createdID)
		task.DependsOn = deps
		task.UpdatedAt = time.Now().UTC()
		manifest.NextSequence++
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
		}
		task = plan.GetTask(createdID)
		if task == nil {
			return ErrTaskNotFound
		}
		copy := *task
		created = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) UpdateTask(planID, taskID string, opts TaskUpdateOptions) (*TaskRecord, error) {
	var updated *TaskRecord
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		task, err := plan.ApplyTaskUpdate(taskID, opts)
		if err != nil {
			return err
		}
		if task.Status != StatusComplete {
			task.ArchivedAt = nil
		}
		if task.Status == StatusComplete || task.Status == StatusCancelled || task.Status == StatusBlocked {
			task.ClaimedBy = ""
			task.ClaimedAt = nil
		}
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
		}
		task = plan.GetTask(taskID)
		if task == nil {
			return ErrTaskNotFound
		}
		copy := *task
		updated = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Store) DeleteTask(planID, taskID string) (int, error) {
	removed := 0
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		removed = plan.DeleteTask(taskID)
		if removed == 0 {
			return nil
		}
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}

func (s *Store) Ready(planID string) ([]TaskRecord, error) {
	plan, _, err := s.loadPlan(planID)
	if err != nil {
		return nil, err
	}
	return plan.ReadyTasks(), nil
}

func (s *Store) NextTask(planID, claimedBy string) (*TaskRecord, error) {
	plan, _, err := s.loadPlan(planID)
	if err != nil {
		return nil, err
	}
	return plan.SelectNextTask(strings.TrimSpace(claimedBy)), nil
}

func (s *Store) ClaimTask(planID, taskID, by string) (*TaskRecord, error) {
	return s.claimTask(planID, taskID, by)
}

func (s *Store) ClaimNext(planID, by string) (*TaskRecord, error) {
	by = strings.TrimSpace(by)
	var claimed *TaskRecord
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		task := plan.SelectNextTask(by)
		if task == nil {
			return nil
		}
		current := plan.GetTask(task.ID)
		if current == nil {
			return ErrTaskNotFound
		}
		if by == "" {
			by = defaultActor()
		}
		if current.ClaimedBy == by {
			if current.Status != StatusInProgress {
				now := time.Now().UTC()
				current.Assignee = by
				current.Status = StatusInProgress
				current.UpdatedAt = now
				if current.ClaimedAt == nil {
					current.ClaimedAt = &now
				}
				claimedID := current.ID
				plan.touch()
				if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
					return err
				}
				current = plan.GetTask(claimedID)
				if current == nil {
					return ErrTaskNotFound
				}
			}
			copy := *current
			claimed = &copy
			return nil
		}
		claimedID := current.ID
		claimPath := s.claimPath(planID, claimedID)
		claim, err := createClaim(claimPath, by)
		if err != nil {
			return err
		}
		now := claim.ClaimedAt.UTC()
		current.Assignee = by
		current.ClaimedBy = by
		current.ClaimedAt = &now
		current.Status = StatusInProgress
		current.UpdatedAt = now
		plan.touch()
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			_ = removeFileFunc(claimPath)
			return err
		}
		current = plan.GetTask(claimedID)
		if current == nil {
			return ErrTaskNotFound
		}
		copy := *current
		claimed = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) ChainTasks(planID string, taskIDs []string) ([]TaskRecord, error) {
	taskIDs = normalizeIDs(taskIDs)
	if len(taskIDs) < 2 {
		return nil, fmt.Errorf("chain requires at least two tasks")
	}
	var updated []TaskRecord
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		for _, taskID := range taskIDs {
			if plan.GetTask(taskID) == nil {
				return fmt.Errorf("task not found: %s", taskID)
			}
		}
		for i := 1; i < len(taskIDs); i++ {
			current := plan.GetTask(taskIDs[i])
			if current == nil {
				return ErrTaskNotFound
			}
			deps := append([]string(nil), current.DependsOn...)
			deps = append(deps, taskIDs[i-1])
			normalized, err := plan.ValidateDependencyIDs(deps, current.ID)
			if err != nil {
				return err
			}
			current.DependsOn = normalized
			current.UpdatedAt = time.Now().UTC()
		}
		plan.touch()
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
		}
		updated = make([]TaskRecord, 0, len(taskIDs))
		for _, taskID := range taskIDs {
			task := plan.GetTask(taskID)
			if task != nil {
				updated = append(updated, *task)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *Store) claimTask(planID, taskID, by string) (*TaskRecord, error) {
	by = strings.TrimSpace(by)
	if by == "" {
		by = defaultActor()
	}
	var claimed *TaskRecord
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		task := plan.GetTask(taskID)
		if task == nil {
			return ErrTaskNotFound
		}
		if task.Status == StatusComplete || task.Status == StatusCancelled {
			return fmt.Errorf("task %s is not claimable from status %s", taskID, task.Status)
		}
		if task.ClaimedBy != "" {
			return ErrTaskAlreadyClaimed
		}
		claimedID := task.ID
		claimPath := s.claimPath(planID, claimedID)
		claim, err := createClaim(claimPath, by)
		if err != nil {
			return err
		}
		now := claim.ClaimedAt.UTC()
		task.Assignee = by
		task.ClaimedBy = by
		task.ClaimedAt = &now
		task.Status = StatusInProgress
		task.UpdatedAt = now
		plan.touch()
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			_ = removeFileFunc(claimPath)
			return err
		}
		task = plan.GetTask(claimedID)
		if task == nil {
			return ErrTaskNotFound
		}
		copy := *task
		claimed = &copy
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) persistPlanLocked(plan *Plan, manifest *Manifest, _ map[string]TaskSummary) error {
	if manifest == nil {
		manifest = &Manifest{
			Version:      currentManifestVersion,
			PlanID:       plan.ID,
			CreatedAt:    plan.CreatedAt,
			UpdatedAt:    plan.UpdatedAt,
			NextSequence: 1,
		}
	}
	if manifest.PlanID == "" {
		manifest.PlanID = plan.ID
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = plan.CreatedAt
	}

	previousFiles := make(map[string]TaskSummary, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		previousFiles[task.ID] = task
	}

	record := PlanRecord{
		ID:          plan.ID,
		Name:        plan.Name,
		Description: plan.Description,
		SpecPath:    strings.TrimSpace(plan.SpecPath),
		CreatedAt:   plan.CreatedAt.UTC(),
		UpdatedAt:   plan.UpdatedAt.UTC(),
		Version:     plan.Version,
	}
	for _, dir := range []string{s.planDir(plan.ID), s.planTasksDir(plan.ID), s.planHistoryDir(plan.ID)} {
		if err := mkdirAllFunc(dir, defaultDirPerm); err != nil {
			return err
		}
	}

	sortTasks(plan.Tasks)
	nextManifest := &Manifest{
		Version:      currentManifestVersion,
		PlanID:       manifest.PlanID,
		CreatedAt:    manifest.CreatedAt.UTC(),
		UpdatedAt:    plan.UpdatedAt.UTC(),
		NextSequence: manifest.NextSequence,
		Tasks:        make([]TaskSummary, 0, len(plan.Tasks)),
	}
	if nextManifest.NextSequence == 0 {
		nextManifest.NextSequence = maxSequence(plan.Tasks) + 1
	}
	currentFiles := make(map[string]TaskSummary, len(plan.Tasks))

	// Durable records and active claims must exist before the new manifest can
	// reference them. Superseded files remain in place until after the commit.
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if task.Name == "" {
			task.Name = fmt.Sprintf("Task %s", task.ID)
		}
		task.Path = taskRecordRelativePath(task.ID, task.Status)
		if task.Status == StatusComplete {
			task.ClaimPath = ""
		} else {
			task.ClaimPath = taskClaimRelativePath(task.ID)
		}
		summary := summaryFromRecord(*task)
		previous, existed := previousFiles[task.ID]
		if !existed || !taskSummariesEqual(previous, summary) {
			recordPath, err := s.resolveManifestRecordPath(plan.ID, task.Path, false)
			if err != nil {
				return err
			}
			if err := s.writeTaskRecord(recordPath, *task); err != nil {
				return err
			}
		}
		if taskHasActiveClaim(*task) && (!existed || claimSummaryChanged(previous, summary)) {
			claimPath, err := s.resolveManifestRecordPath(plan.ID, task.ClaimPath, true)
			if err != nil {
				return err
			}
			claimedAt := time.Now().UTC()
			if task.ClaimedAt != nil {
				claimedAt = task.ClaimedAt.UTC()
			}
			if err := atomicWriteJSON(claimPath, ClaimInfo{
				By:        task.ClaimedBy,
				ClaimedAt: claimedAt,
				Host:      os.Getenv("HOSTNAME"),
				PID:       os.Getpid(),
			}); err != nil {
				return err
			}
		}
		currentFiles[task.ID] = summary
		nextManifest.Tasks = append(nextManifest.Tasks, summary)
	}
	sortSummaries(nextManifest.Tasks)

	// Committing metadata happens only after every referenced record exists.
	// A failure leaves the previous manifest and all of its paths readable.
	if err := atomicWriteJSON(s.planInfoPath(plan.ID), record); err != nil {
		return err
	}
	if err := atomicWriteJSON(s.planManifestPath(plan.ID), nextManifest); err != nil {
		return err
	}

	// Cleanup is deliberately best effort. A leftover file is an auditable
	// orphan; removing an active destination would be data loss.
	for id, previous := range previousFiles {
		current, retained := currentFiles[id]
		if previous.Path != "" && (!retained || previous.Path != current.Path) {
			s.cleanupSupersededRecord(plan.ID, previous.Path, current.Path)
		}
		if previous.ClaimPath != "" && (!retained || previous.ClaimPath != current.ClaimPath || !taskSummaryHasActiveClaim(current)) {
			activeClaimPath := current.ClaimPath
			if !retained || !taskSummaryHasActiveClaim(current) {
				activeClaimPath = ""
			}
			s.cleanupSupersededRecord(plan.ID, previous.ClaimPath, activeClaimPath)
		}
	}

	*manifest = *nextManifest
	return nil
}

func (s *Store) loadPlan(planID string) (*Plan, *Manifest, error) {
	record, err := s.loadPlanRecord(planID)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := s.loadManifest(planID)
	if err != nil {
		return nil, nil, err
	}
	records := make([]TaskRecord, 0, len(manifest.Tasks))
	for _, summary := range manifest.Tasks {
		path := summary.Path
		if path == "" {
			path = taskRecordRelativePath(summary.ID, summary.Status)
		}
		recordPath, err := s.resolveManifestRecordPath(planID, path, false)
		if err != nil {
			return nil, nil, err
		}
		recordTask, err := s.readTaskRecord(recordPath)
		if err != nil {
			code := ErrorCodePlanUnreadable
			message := "task record is unreadable"
			if errors.Is(err, os.ErrNotExist) {
				code = ErrorCodeTaskRecordMissing
				message = "task record missing"
			}
			return nil, nil, &PlanError{
				ErrorCode: code,
				Message:   message,
				PlanID:    planID,
				TaskID:    summary.ID,
				Path:      summary.Path,
				Err:       err,
			}
		}
		recordTask.Path = path
		if summary.ClaimPath != "" {
			recordTask.ClaimPath = summary.ClaimPath
		} else if recordTask.Status != StatusComplete {
			recordTask.ClaimPath = taskClaimRelativePath(recordTask.ID)
		}
		if recordTask.Status != StatusComplete {
			claimPath, err := s.resolveManifestRecordPath(planID, recordTask.ClaimPath, true)
			if err != nil {
				return nil, nil, err
			}
			if data, err := readFileFunc(claimPath); err == nil {
				var claim ClaimInfo
				if err := json.Unmarshal(data, &claim); err == nil {
					recordTask.ClaimedBy = claim.By
					recordTask.Assignee = claim.By
					claimedAt := claim.ClaimedAt.UTC()
					recordTask.ClaimedAt = &claimedAt
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, nil, &PlanError{
					ErrorCode: ErrorCodePlanUnreadable,
					Message:   "task claim is unreadable",
					PlanID:    planID,
					TaskID:    summary.ID,
					Path:      recordTask.ClaimPath,
					Err:       err,
				}
			}
		}
		records = append(records, recordTask)
	}
	plan := &Plan{
		ID:          record.ID,
		Name:        record.Name,
		Description: record.Description,
		SpecPath:    record.SpecPath,
		Tasks:       records,
		CreatedAt:   record.CreatedAt.UTC(),
		UpdatedAt:   record.UpdatedAt.UTC(),
		Version:     record.Version,
	}
	return plan, &manifest, nil
}

func (s *Store) loadPlanRecord(planID string) (PlanRecord, error) {
	data, err := readFileFunc(s.planInfoPath(planID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return PlanRecord{}, &PlanError{
				ErrorCode: ErrorCodePlanNotFound,
				Message:   "plan not found",
				PlanID:    planID,
				Err:       err,
			}
		}
		return PlanRecord{}, &PlanError{
			ErrorCode: ErrorCodePlanUnreadable,
			Message:   "plan record is unreadable",
			PlanID:    planID,
			Err:       err,
		}
	}
	var record PlanRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return PlanRecord{}, &PlanError{
			ErrorCode: ErrorCodePlanUnreadable,
			Message:   "plan record is invalid",
			PlanID:    planID,
			Err:       err,
		}
	}
	return record, nil
}

func (s *Store) loadManifest(planID string) (Manifest, error) {
	data, err := readFileFunc(s.planManifestPath(planID))
	if err != nil {
		return Manifest{}, &PlanError{
			ErrorCode: ErrorCodeManifestInvalid,
			Message:   "manifest is unreadable",
			PlanID:    planID,
			Path:      manifestFileName,
			Err:       err,
		}
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, &PlanError{
			ErrorCode: ErrorCodeManifestInvalid,
			Message:   "manifest is invalid",
			PlanID:    planID,
			Path:      manifestFileName,
			Err:       err,
		}
	}
	if manifest.Version == 0 {
		manifest.Version = 1
	}
	if manifest.Version != 1 && manifest.Version != currentManifestVersion {
		return Manifest{}, &PlanError{
			ErrorCode: ErrorCodeManifestInvalid,
			Message:   fmt.Sprintf("unsupported manifest version %d", manifest.Version),
			PlanID:    planID,
			Path:      manifestFileName,
		}
	}
	if manifest.Version == 1 {
		s.addWarning("version 1 manifest loaded; the next successful mutation will upgrade it to version 2")
	}
	if manifest.PlanID != "" && manifest.PlanID != planID {
		return Manifest{}, &PlanError{
			ErrorCode: ErrorCodeManifestInvalid,
			Message:   "manifest plan_id does not match plan",
			PlanID:    planID,
			Path:      manifestFileName,
		}
	}
	for i := range manifest.Tasks {
		summary := &manifest.Tasks[i]
		if summary.ID == "" {
			return Manifest{}, &PlanError{
				ErrorCode: ErrorCodeManifestInvalid,
				Message:   fmt.Sprintf("manifest task %d has no id", i),
				PlanID:    planID,
				Path:      manifestFileName,
			}
		}
		expectedTaskPath := taskRecordRelativePath(summary.ID, summary.Status)
		if summary.Path == "" {
			summary.Path = expectedTaskPath
		} else if manifest.Version == 1 {
			relative, err := s.legacyManifestRelativePath(planID, summary.Path, expectedTaskPath, false)
			if err != nil {
				return Manifest{}, err
			}
			summary.Path = relative
		} else if _, err := s.resolveManifestRecordPath(planID, summary.Path, false); err != nil {
			return Manifest{}, err
		}
		if summary.Path != expectedTaskPath {
			return Manifest{}, &PlanError{
				ErrorCode: ErrorCodeManifestInvalid,
				Message:   "manifest task path does not match its id and status",
				PlanID:    planID,
				TaskID:    summary.ID,
				Path:      summary.Path,
			}
		}
		if summary.Status == StatusComplete {
			summary.ClaimPath = ""
		} else if summary.ClaimPath == "" {
			summary.ClaimPath = taskClaimRelativePath(summary.ID)
		} else if manifest.Version == 1 {
			relative, err := s.legacyManifestRelativePath(planID, summary.ClaimPath, taskClaimRelativePath(summary.ID), true)
			if err != nil {
				return Manifest{}, err
			}
			summary.ClaimPath = relative
		} else if _, err := s.resolveManifestRecordPath(planID, summary.ClaimPath, true); err != nil {
			return Manifest{}, err
		}
		if summary.ClaimPath != "" && summary.ClaimPath != taskClaimRelativePath(summary.ID) {
			return Manifest{}, &PlanError{
				ErrorCode: ErrorCodeManifestInvalid,
				Message:   "manifest claim path does not match its task id",
				PlanID:    planID,
				TaskID:    summary.ID,
				Path:      summary.ClaimPath,
			}
		}
	}
	if manifest.NextSequence == 0 {
		manifest.NextSequence = 1
	}
	return manifest, nil
}

func (s *Store) ensureWorkspaceLocked() error {
	if err := mkdirAllFunc(s.MetaDir, defaultDirPerm); err != nil {
		return err
	}
	if err := mkdirAllFunc(s.plansDir(), defaultDirPerm); err != nil {
		return err
	}
	if _, err := statFunc(s.workspacePath()); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	now := time.Now().UTC()
	workspace := Workspace{
		Version:   currentStoreFormat,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return s.saveWorkspace(workspace)
}

func (s *Store) loadWorkspace() (Workspace, error) {
	data, err := readFileFunc(s.workspacePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Workspace{}, ErrPlanNotInitialized
		}
		return Workspace{}, err
	}
	var workspace Workspace
	if err := json.Unmarshal(data, &workspace); err != nil {
		return Workspace{}, err
	}
	if workspace.Version == 0 {
		workspace.Version = currentStoreFormat
	}
	return workspace, nil
}

func (s *Store) saveWorkspace(workspace Workspace) error {
	if err := mkdirAllFunc(s.MetaDir, defaultDirPerm); err != nil {
		return err
	}
	return atomicWriteJSON(s.workspacePath(), workspace)
}

func (s *Store) readTaskRecord(path string) (TaskRecord, error) {
	data, err := readFileFunc(path)
	if err != nil {
		return TaskRecord{}, err
	}
	record, err := parseTaskRecord(data)
	if err != nil {
		return TaskRecord{}, err
	}
	return record, nil
}

func (s *Store) writeTaskRecord(path string, record TaskRecord) error {
	if err := mkdirAllFunc(filepath.Dir(path), defaultDirPerm); err != nil {
		return err
	}
	data, err := marshalTaskRecord(record)
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, defaultFilePerm)
}

func (s *Store) withWorkspaceLock(fn func() error) error {
	return withLock(s.workspaceLockPath(), fn)
}

func (s *Store) withPlanLock(planID string, fn func() error) error {
	if err := mkdirAllFunc(s.planDir(planID), defaultDirPerm); err != nil {
		return err
	}
	return withLock(s.planLockPath(planID), fn)
}

func withLock(path string, fn func() error) error {
	if err := mkdirAllFunc(filepath.Dir(path), defaultDirPerm); err != nil {
		return err
	}
	lockPayload := map[string]any{
		"pid":        os.Getpid(),
		"host":       os.Getenv("HOSTNAME"),
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	deadline := time.Now().Add(lockWaitTimeout)
	for {
		err := writeExclusiveJSON(path, lockPayload)
		if err == nil {
			defer func() { _ = removeFileFunc(path) }()
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for lock")
		}
		time.Sleep(lockPollInterval)
	}
}

func createClaim(path, by string) (ClaimInfo, error) {
	claim := ClaimInfo{
		By:        by,
		ClaimedAt: time.Now().UTC(),
		Host:      os.Getenv("HOSTNAME"),
		PID:       os.Getpid(),
	}
	if err := writeExclusiveJSON(path, claim); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ClaimInfo{}, ErrTaskAlreadyClaimed
		}
		return ClaimInfo{}, err
	}
	return claim, nil
}

func writeExclusiveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeExclusiveFile(path, data, defaultFilePerm)
}

func atomicWriteJSON(path string, value any) error {
	data, err := jsonMarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, data, defaultFilePerm)
}

func writeExclusiveFile(path string, data []byte, perm os.FileMode) error {
	if err := mkdirAllFunc(filepath.Dir(path), defaultDirPerm); err != nil {
		return err
	}
	file, err := openExclusiveFileFunc(path, perm)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return err
	}
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if err := mkdirAllFunc(filepath.Dir(path), defaultDirPerm); err != nil {
		return err
	}
	tmp, err := createTempFileFunc(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer removeFileFunc(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return renameFunc(tmpPath, path)
}

func selectRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		if envRoot := strings.TrimSpace(os.Getenv("AGENT_PLAN_DIR")); envRoot != "" {
			root = envRoot
		}
	}
	if strings.TrimSpace(root) == "" {
		cwd, err := getwdFunc()
		if err != nil {
			return "", err
		}
		root = cwd
	}
	resolved, err := absFunc(root)
	if err != nil {
		return "", err
	}
	if filepath.Base(resolved) == metaDirName {
		return filepath.Dir(resolved), nil
	}
	return filepath.Clean(resolved), nil
}

func normalizeRoot(root string) (string, error) {
	selected, err := selectRoot(root)
	if err != nil {
		return "", err
	}
	return canonicalizePath(selected)
}

func canonicalizePath(path string) (string, error) {
	path = filepath.Clean(path)
	current := path
	missing := make([]string, 0)
	for {
		if _, err := lstatFunc(current); err == nil {
			resolved, err := evalSymlinksFunc(current)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("cannot resolve workspace root %q", path)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func validateMetaDirWithinRoot(root, metaDir string) error {
	if _, err := lstatFunc(metaDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	resolvedMeta, err := evalSymlinksFunc(metaDir)
	if err != nil {
		return err
	}
	if !pathWithin(root, resolvedMeta) {
		return fmt.Errorf("%s resolves outside workspace %s", metaDir, root)
	}
	return nil
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func findWorkspaceRoot(root string) (string, error) {
	start, err := normalizeRoot(root)
	if err != nil {
		return "", err
	}
	current := start
	for {
		metaDir := filepath.Join(current, metaDirName)
		if _, err := statFunc(metaDir); err == nil {
			if err := validateMetaDirWithinRoot(current, metaDir); err != nil {
				return "", err
			}
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", ErrPlanNotInitialized
		}
		current = parent
	}
}

func normalizeIDs(ids []string) []string {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(ids))
	for _, id := range ids {
		cleaned := strings.TrimSpace(id)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	return normalized
}

func sortSummaries(tasks []TaskSummary) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Order != tasks[j].Order {
			return tasks[i].Order < tasks[j].Order
		}
		return tasks[i].Sequence < tasks[j].Sequence
	})
}

func summaryFromRecord(record TaskRecord) TaskSummary {
	return TaskSummary{
		ID:                 record.ID,
		Sequence:           record.Sequence,
		ImportKey:          record.ImportKey,
		Name:               record.Name,
		Status:             record.Status,
		Order:              record.Order,
		ParentID:           record.ParentID,
		DependsOn:          normalizeIDs(record.DependsOn),
		Assignee:           record.Assignee,
		BlockedReason:      record.BlockedReason,
		CreatedAt:          record.CreatedAt.UTC(),
		UpdatedAt:          record.UpdatedAt.UTC(),
		ClaimedBy:          record.ClaimedBy,
		ClaimedAt:          record.ClaimedAt,
		CompletedAt:        record.CompletedAt,
		ArchivedAt:         record.ArchivedAt,
		Path:               record.Path,
		ClaimPath:          record.ClaimPath,
		DescriptionPreview: preview(record.Description),
		RecordHash:         taskRecordHash(record),
	}
}

func taskRecordHash(record TaskRecord) string {
	data, err := json.Marshal(record)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func parseTaskRecord(data []byte) (TaskRecord, error) {
	text := string(data)
	if !strings.HasPrefix(text, "---\n") {
		return TaskRecord{}, fmt.Errorf("task file missing yaml frontmatter")
	}
	rest := strings.TrimPrefix(text, "---\n")
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return TaskRecord{}, fmt.Errorf("task file frontmatter is not terminated")
	}
	frontmatter := rest[:end]
	body := rest[end+5:]
	type legacyTaskRecord struct {
		TaskRecord `yaml:",inline"`
		Title      string `yaml:"title"`
	}
	var record legacyTaskRecord
	if err := yamlUnmarshalFunc([]byte(frontmatter), &record); err != nil {
		return TaskRecord{}, err
	}
	task := record.TaskRecord
	if task.Name == "" {
		task.Name = strings.TrimSpace(record.Title)
	}
	task.Description = strings.TrimSpace(body)
	return task, nil
}

func marshalTaskRecord(record TaskRecord) ([]byte, error) {
	record.Path = ""
	record.ClaimPath = ""
	body := strings.TrimSpace(record.Description)
	frontmatter, err := yamlMarshalFunc(record)
	if err != nil {
		return nil, err
	}
	builder := strings.Builder{}
	builder.WriteString("---\n")
	builder.Write(frontmatter)
	builder.WriteString("---\n")
	if body != "" {
		builder.WriteString("\n")
		builder.WriteString(body)
		builder.WriteString("\n")
	}
	return []byte(builder.String()), nil
}

func randomID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func maxSequence(tasks []TaskRecord) int {
	max := 0
	for _, task := range tasks {
		if task.Sequence > max {
			max = task.Sequence
		}
	}
	return max
}

func slugify(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var builder strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "task"
	}
	return result
}

func preview(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	line := strings.Split(text, "\n")[0]
	if len(line) <= 120 {
		return line
	}
	return line[:117] + "..."
}

func defaultActor() string {
	for _, key := range []string{
		"AGENT_PLAN_ASSIGNEE",
		"AGENT_NAME",
		"AGENT_SESSION_ID",
	} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	user := strings.TrimSpace(os.Getenv("USER"))
	host := strings.TrimSpace(os.Getenv("HOSTNAME"))
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case user != "":
		return user
	case host != "":
		return host
	default:
		return fmt.Sprintf("pid-%d", os.Getpid())
	}
}

func taskPathForStatus(id string, status Status) string {
	if status == StatusComplete {
		return filepath.Join(historyDirName, id+".done")
	}
	return filepath.Join(tasksDirName, id+".md")
}

func (s *Store) workspacePath() string        { return filepath.Join(s.MetaDir, workspaceFileName) }
func (s *Store) workspaceLockPath() string    { return filepath.Join(s.MetaDir, workspaceLockName) }
func (s *Store) plansDir() string             { return filepath.Join(s.MetaDir, plansDirName) }
func (s *Store) planDir(planID string) string { return filepath.Join(s.plansDir(), planID) }
func (s *Store) planInfoPath(planID string) string {
	return filepath.Join(s.planDir(planID), planFileName)
}
func (s *Store) planManifestPath(planID string) string {
	return filepath.Join(s.planDir(planID), manifestFileName)
}
func (s *Store) planLockPath(planID string) string {
	return filepath.Join(s.planDir(planID), manifestLockName)
}
func (s *Store) planTasksDir(planID string) string {
	return filepath.Join(s.planDir(planID), tasksDirName)
}
func (s *Store) planHistoryDir(planID string) string {
	return filepath.Join(s.planDir(planID), historyDirName)
}
func (s *Store) taskPath(planID, id string) string {
	return filepath.Join(s.planTasksDir(planID), id+".md")
}
func (s *Store) claimPath(planID, id string) string {
	return filepath.Join(s.planTasksDir(planID), id+".claim")
}
func (s *Store) historyPath(planID, id string) string {
	return filepath.Join(s.planHistoryDir(planID), id+".done")
}
func (s *Store) taskFilePath(planID, id string, status Status) string {
	if status == StatusComplete {
		return s.historyPath(planID, id)
	}
	return s.taskPath(planID, id)
}

func (s *Store) addWarning(message string) {
	for _, warning := range s.warnings {
		if warning == message {
			return
		}
	}
	s.warnings = append(s.warnings, message)
}

func (s *Store) Warnings() []string {
	return append([]string(nil), s.warnings...)
}

func taskRecordRelativePath(id string, status Status) string {
	if status == StatusComplete {
		return filepath.ToSlash(filepath.Join(historyDirName, id+".done"))
	}
	return filepath.ToSlash(filepath.Join(tasksDirName, id+".md"))
}

func taskClaimRelativePath(id string) string {
	return filepath.ToSlash(filepath.Join(tasksDirName, id+".claim"))
}

func taskHasActiveClaim(task TaskRecord) bool {
	return task.ClaimedBy != "" &&
		task.Status != StatusComplete &&
		task.Status != StatusCancelled &&
		task.Status != StatusBlocked
}

func taskSummaryHasActiveClaim(task TaskSummary) bool {
	return task.ClaimedBy != "" &&
		task.Status != StatusComplete &&
		task.Status != StatusCancelled &&
		task.Status != StatusBlocked
}

func claimSummaryChanged(previous, current TaskSummary) bool {
	return previous.ClaimPath != current.ClaimPath ||
		previous.ClaimedBy != current.ClaimedBy ||
		!reflect.DeepEqual(previous.ClaimedAt, current.ClaimedAt)
}

func taskSummariesEqual(first, second TaskSummary) bool {
	first.DependsOn = normalizeIDs(first.DependsOn)
	second.DependsOn = normalizeIDs(second.DependsOn)
	return reflect.DeepEqual(first, second)
}

func (s *Store) resolveManifestRecordPath(planID, recordPath string, claim bool) (string, error) {
	pathError := func(message string, err error) error {
		return &PlanError{
			ErrorCode: ErrorCodePathOutsidePlan,
			Message:   message,
			PlanID:    planID,
			Path:      recordPath,
			Err:       err,
		}
	}
	if strings.TrimSpace(recordPath) == "" {
		return "", pathError("manifest record path is required", nil)
	}
	if filepath.IsAbs(recordPath) {
		return "", pathError("manifest version 2 path must be relative", nil)
	}
	cleaned := filepath.Clean(filepath.FromSlash(recordPath))
	if cleaned == "." || cleaned != filepath.FromSlash(recordPath) {
		return "", pathError("manifest record path is not clean", nil)
	}
	parts := strings.Split(filepath.ToSlash(cleaned), "/")
	if len(parts) != 2 || parts[0] == ".." || parts[1] == "" {
		return "", pathError("manifest record path is not allowed", nil)
	}
	switch {
	case claim && (parts[0] != tasksDirName || !strings.HasSuffix(parts[1], ".claim")):
		return "", pathError("manifest claim path is not allowed", nil)
	case !claim && parts[0] == tasksDirName && !strings.HasSuffix(parts[1], ".md"):
		return "", pathError("manifest task path is not allowed", nil)
	case !claim && parts[0] == historyDirName && !strings.HasSuffix(parts[1], ".done"):
		return "", pathError("manifest history path is not allowed", nil)
	case !claim && parts[0] != tasksDirName && parts[0] != historyDirName:
		return "", pathError("manifest record path is not allowed", nil)
	}
	planDir, err := canonicalizePath(s.planDir(planID))
	if err != nil {
		return "", pathError("canonical plan directory is unreadable", err)
	}
	absolute := filepath.Join(planDir, cleaned)
	resolved, err := canonicalizePath(absolute)
	if err != nil {
		return "", pathError("manifest record path cannot be resolved", err)
	}
	if !pathWithin(planDir, resolved) {
		return "", pathError("manifest record path resolves outside plan", nil)
	}
	return absolute, nil
}

func (s *Store) legacyManifestRelativePath(planID, recordPath, expectedPath string, claim bool) (string, error) {
	if !filepath.IsAbs(recordPath) {
		if _, err := s.resolveManifestRecordPath(planID, recordPath, claim); err != nil {
			return "", err
		}
		return filepath.ToSlash(filepath.Clean(recordPath)), nil
	}
	planDir, err := canonicalizePath(s.planDir(planID))
	if err != nil {
		return "", err
	}
	resolved, err := canonicalizePath(recordPath)
	if err != nil {
		return "", err
	}
	if !pathWithin(planDir, resolved) {
		if legacyManifestPathMatchesExpectedRecord(planID, recordPath, expectedPath) {
			relative := filepath.ToSlash(filepath.Clean(filepath.FromSlash(expectedPath)))
			if _, err := s.resolveManifestRecordPath(planID, relative, claim); err != nil {
				return "", err
			}
			s.addWarning("version 1 manifest contains relocated absolute paths; using matching records from the current plan")
			return relative, nil
		}
		return "", &PlanError{
			ErrorCode: ErrorCodePathOutsidePlan,
			Message:   "legacy manifest path resolves outside plan",
			PlanID:    planID,
			Path:      recordPath,
		}
	}
	relative, err := filepath.Rel(planDir, resolved)
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(relative)
	if _, err := s.resolveManifestRecordPath(planID, relative, claim); err != nil {
		return "", err
	}
	return relative, nil
}

func legacyManifestPathMatchesExpectedRecord(planID, recordPath, expectedPath string) bool {
	recordPath = filepath.Clean(recordPath)
	expectedPath = filepath.Clean(filepath.FromSlash(expectedPath))
	return filepath.Base(filepath.Dir(filepath.Dir(recordPath))) == planID &&
		filepath.Base(recordPath) == filepath.Base(expectedPath) &&
		filepath.Base(filepath.Dir(recordPath)) == filepath.Base(filepath.Dir(expectedPath))
}

func (s *Store) cleanupSupersededRecord(planID, previousPath, activePath string) {
	claim := strings.HasSuffix(previousPath, ".claim")
	previous, err := s.resolveManifestRecordPath(planID, previousPath, claim)
	if err != nil {
		s.addWarning(fmt.Sprintf("cleanup retained %q: %v", previousPath, err))
		return
	}
	if activePath != "" {
		active, err := s.resolveManifestRecordPath(planID, activePath, strings.HasSuffix(activePath, ".claim"))
		if err != nil {
			s.addWarning(fmt.Sprintf("cleanup retained %q: active destination is unsafe: %v", previousPath, err))
			return
		}
		same, known := sameFileIdentity(previous, active)
		if !known {
			s.addWarning(fmt.Sprintf("cleanup retained %q: filesystem identity is unknown", previousPath))
			return
		}
		if same {
			return
		}
	}
	if err := removeFileFunc(previous); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.addWarning(fmt.Sprintf("cleanup left orphan %q: %v", previousPath, err))
	}
}

func sameFileIdentity(first, second string) (same bool, known bool) {
	firstInfo, firstErr := statFunc(first)
	secondInfo, secondErr := statFunc(second)
	if errors.Is(firstErr, os.ErrNotExist) {
		return false, true
	}
	if firstErr != nil || secondErr != nil {
		return false, false
	}
	return os.SameFile(firstInfo, secondInfo), true
}
