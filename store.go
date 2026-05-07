package agentplan

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"crypto/rand"

	"gopkg.in/yaml.v3"
)

const (
	metaDirName        = ".agent-plan"
	plansDirName       = "plans"
	sessionsDirName    = "sessions"
	workspaceFileName  = "workspace.json"
	workspaceLockName  = "workspace.lock"
	planFileName       = "plan.json"
	tasksDirName       = "tasks"
	historyDirName     = "history"
	manifestFileName   = "manifest.json"
	manifestLockName   = "manifest.lock"
	defaultFilePerm    = 0o644
	defaultDirPerm     = 0o755
	currentStoreFormat = 2
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
	Root    string
	MetaDir string
}

func Init(root string) (*Store, error) {
	resolvedRoot, err := normalizeRoot(root)
	if err != nil {
		return nil, err
	}
	store := &Store{
		Root:    resolvedRoot,
		MetaDir: filepath.Join(resolvedRoot, metaDirName),
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
			Version:      1,
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
			SessionIDs:    append([]string(nil), planRecord.SessionIDs...),
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
		if err := s.removeBindingsForPlan(planID); err != nil {
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
		deps, err := plan.ValidateDependencyIDs(opts.DependsOn, record.ID)
		if err != nil {
			return err
		}
		task := plan.GetTask(record.ID)
		task.DependsOn = deps
		task.UpdatedAt = time.Now().UTC()
		manifest.NextSequence++
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
		}
		task = plan.GetTask(record.ID)
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

func (s *Store) NextTask(planID, sessionID string) (*TaskRecord, error) {
	plan, _, err := s.loadPlan(planID)
	if err != nil {
		return nil, err
	}
	return plan.SelectNextTask(strings.TrimSpace(sessionID)), nil
}

func (s *Store) ClaimTask(planID, taskID, by string) (*TaskRecord, error) {
	return s.claimTask(planID, taskID, by, "")
}

func (s *Store) ClaimNext(planID, sessionID string) (*TaskRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	var claimed *TaskRecord
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		task := plan.SelectNextTask(sessionID)
		if task == nil {
			return nil
		}
		current := plan.GetTask(task.ID)
		if current == nil {
			return ErrTaskNotFound
		}
		if sessionID != "" {
			current.AssignedSessionID = sessionID
		}
		by := sessionID
		if by == "" {
			by = defaultActor()
		}
		claimPath := s.claimPath(planID, current.ID)
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
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
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

func (s *Store) BindSession(planID, sessionID string) (*SessionBinding, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	var binding *SessionBinding
	err := s.withWorkspaceLock(func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		if !containsID(plan.SessionIDs, sessionID) {
			plan.SessionIDs = append(plan.SessionIDs, sessionID)
			plan.touch()
			if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
				return err
			}
		}
		if err := s.removeSessionFromOtherPlans(plan.ID, sessionID); err != nil {
			return err
		}
		currentTask := firstAssignedTask(plan, sessionID)
		payload := &SessionBinding{
			SessionID: sessionID,
			PlanID:    plan.ID,
			PlanName:  plan.Name,
			UpdatedAt: time.Now().UTC(),
		}
		if currentTask != nil {
			payload.CurrentTaskID = currentTask.ID
			payload.CurrentTaskName = currentTask.Name
			payload.ClaimedAt = currentTask.ClaimedAt
		}
		if err := s.saveSessionBinding(payload); err != nil {
			return err
		}
		binding = payload
		return nil
	})
	if err != nil {
		return nil, err
	}
	return binding, nil
}

func (s *Store) BindCurrent(planID string) (*SessionBinding, error) {
	sessionID := resolveCurrentSessionID()
	if sessionID == "" {
		return nil, fmt.Errorf("no current session env was found")
	}
	return s.BindSession(planID, sessionID)
}

func (s *Store) GetSessionBinding(sessionID string) (*SessionBinding, error) {
	data, err := readFileFunc(s.sessionBindingPath(sessionID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var binding SessionBinding
	if err := json.Unmarshal(data, &binding); err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s *Store) claimTask(planID, taskID, by, assignedSessionID string) (*TaskRecord, error) {
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
		claim, err := createClaim(s.claimPath(planID, task.ID), by)
		if err != nil {
			return err
		}
		now := claim.ClaimedAt.UTC()
		task.Assignee = by
		task.ClaimedBy = by
		task.ClaimedAt = &now
		task.Status = StatusInProgress
		task.UpdatedAt = now
		if assignedSessionID != "" {
			task.AssignedSessionID = assignedSessionID
		}
		if err := s.persistPlanLocked(plan, manifest, nil); err != nil {
			return err
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
			Version:      1,
			PlanID:       plan.ID,
			CreatedAt:    plan.CreatedAt,
			UpdatedAt:    plan.UpdatedAt,
			NextSequence: 1,
		}
	}
	if manifest.Version == 0 {
		manifest.Version = 1
	}
	if manifest.PlanID == "" {
		manifest.PlanID = plan.ID
	}
	if manifest.CreatedAt.IsZero() {
		manifest.CreatedAt = plan.CreatedAt
	}

	previousFiles := map[string]TaskSummary{}
	for _, task := range manifest.Tasks {
		previousFiles[task.ID] = task
	}

	record := PlanRecord{
		ID:          plan.ID,
		Name:        plan.Name,
		Description: plan.Description,
		SpecPath:    strings.TrimSpace(plan.SpecPath),
		SessionIDs:  normalizeIDs(plan.SessionIDs),
		CreatedAt:   plan.CreatedAt.UTC(),
		UpdatedAt:   plan.UpdatedAt.UTC(),
		Version:     plan.Version,
	}
	if err := mkdirAllFunc(s.planDir(plan.ID), defaultDirPerm); err != nil {
		return err
	}
	if err := mkdirAllFunc(s.planTasksDir(plan.ID), defaultDirPerm); err != nil {
		return err
	}
	if err := mkdirAllFunc(s.planHistoryDir(plan.ID), defaultDirPerm); err != nil {
		return err
	}
	if err := mkdirAllFunc(s.sessionsDir(), defaultDirPerm); err != nil {
		return err
	}

	current := map[string]TaskSummary{}
	manifest.Tasks = manifest.Tasks[:0]
	sortTasks(plan.Tasks)
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if task.Name == "" {
			task.Name = fmt.Sprintf("Task %s", task.ID)
		}
		task.Path = s.taskFilePath(plan.ID, task.ID, task.Status)
		if task.Status == StatusComplete {
			task.ClaimPath = ""
		} else {
			task.ClaimPath = s.claimPath(plan.ID, task.ID)
		}
		if err := s.writeTaskRecord(task.Path, *task); err != nil {
			return err
		}
		if task.ClaimedBy != "" && task.Status != StatusComplete && task.Status != StatusCancelled && task.Status != StatusBlocked {
			claimedAt := time.Now().UTC()
			if task.ClaimedAt != nil {
				claimedAt = task.ClaimedAt.UTC()
			}
			if err := atomicWriteJSON(task.ClaimPath, ClaimInfo{
				By:        task.ClaimedBy,
				ClaimedAt: claimedAt,
				Host:      os.Getenv("HOSTNAME"),
				PID:       os.Getpid(),
			}); err != nil {
				return err
			}
		} else if task.ClaimPath != "" {
			if err := removeFileFunc(task.ClaimPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else {
			if err := removeFileFunc(s.claimPath(plan.ID, task.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if previous, ok := previousFiles[task.ID]; ok {
			if previous.Path != "" && previous.Path != task.Path {
				if err := removeFileFunc(previous.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			if previous.ClaimPath != "" && previous.ClaimPath != task.ClaimPath {
				if err := removeFileFunc(previous.ClaimPath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
		summary := summaryFromRecord(*task)
		current[task.ID] = summary
		manifest.Tasks = append(manifest.Tasks, summary)
	}
	for id, previous := range previousFiles {
		if _, ok := current[id]; ok {
			continue
		}
		if previous.Path != "" {
			if err := removeFileFunc(previous.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if previous.ClaimPath != "" {
			if err := removeFileFunc(previous.ClaimPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	sortSummaries(manifest.Tasks)
	manifest.UpdatedAt = plan.UpdatedAt.UTC()
	if manifest.NextSequence == 0 {
		manifest.NextSequence = maxSequence(plan.Tasks) + 1
	}

	if err := atomicWriteJSON(s.planInfoPath(plan.ID), record); err != nil {
		return err
	}
	if err := atomicWriteJSON(s.planManifestPath(plan.ID), manifest); err != nil {
		return err
	}
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
			path = s.taskFilePath(planID, summary.ID, summary.Status)
		}
		recordTask, err := s.readTaskRecord(path)
		if err != nil {
			return nil, nil, err
		}
		if summary.ClaimPath != "" {
			recordTask.ClaimPath = summary.ClaimPath
		} else if recordTask.Status != StatusComplete {
			recordTask.ClaimPath = s.claimPath(planID, recordTask.ID)
		}
		if recordTask.Status != StatusComplete {
			if data, err := readFileFunc(s.claimPath(planID, recordTask.ID)); err == nil {
				var claim ClaimInfo
				if err := json.Unmarshal(data, &claim); err == nil {
					recordTask.ClaimedBy = claim.By
					recordTask.Assignee = claim.By
					claimedAt := claim.ClaimedAt.UTC()
					recordTask.ClaimedAt = &claimedAt
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return nil, nil, err
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
		SessionIDs:  append([]string(nil), record.SessionIDs...),
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
			return PlanRecord{}, ErrPlanNotInitialized
		}
		return PlanRecord{}, err
	}
	var record PlanRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return PlanRecord{}, err
	}
	return record, nil
}

func (s *Store) loadManifest(planID string) (Manifest, error) {
	data, err := readFileFunc(s.planManifestPath(planID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, ErrPlanNotInitialized
		}
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Version == 0 {
		manifest.Version = 1
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
	if err := mkdirAllFunc(s.sessionsDir(), defaultDirPerm); err != nil {
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

func (s *Store) removeSessionFromOtherPlans(planID, sessionID string) error {
	plans, err := s.ListPlans()
	if err != nil {
		return err
	}
	for _, summary := range plans {
		if summary.ID == planID || !containsID(summary.SessionIDs, sessionID) {
			continue
		}
		currentPlan, manifest, err := s.loadPlan(summary.ID)
		if err != nil {
			return err
		}
		filtered := make([]string, 0, len(currentPlan.SessionIDs))
		for _, candidate := range currentPlan.SessionIDs {
			if candidate != sessionID {
				filtered = append(filtered, candidate)
			}
		}
		currentPlan.SessionIDs = filtered
		currentPlan.touch()
		if err := s.persistPlanLocked(currentPlan, manifest, nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) removeBindingsForPlan(planID string) error {
	entries, err := readDirFunc(s.sessionsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(s.sessionsDir(), entry.Name())
		data, err := readFileFunc(path)
		if err != nil {
			return err
		}
		var binding SessionBinding
		if err := json.Unmarshal(data, &binding); err != nil {
			return err
		}
		if binding.PlanID != planID {
			continue
		}
		if err := removeFileFunc(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (s *Store) saveSessionBinding(binding *SessionBinding) error {
	return atomicWriteJSON(s.sessionBindingPath(binding.SessionID), binding)
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
	record.Path = path
	record.ClaimPath = filepath.Join(filepath.Dir(path), record.ID+".claim")
	if record.Status == StatusComplete {
		record.ClaimPath = ""
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

func normalizeRoot(root string) (string, error) {
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
	return resolved, nil
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
		Name:               record.Name,
		Status:             record.Status,
		Order:              record.Order,
		ParentID:           record.ParentID,
		AssignedSessionID:  record.AssignedSessionID,
		DependsOn:          normalizeIDs(record.DependsOn),
		Assignee:           record.Assignee,
		BlockedReason:      record.BlockedReason,
		Output:             record.Output,
		CreatedAt:          record.CreatedAt.UTC(),
		UpdatedAt:          record.UpdatedAt.UTC(),
		ClaimedBy:          record.ClaimedBy,
		ClaimedAt:          record.ClaimedAt,
		CompletedAt:        record.CompletedAt,
		ArchivedAt:         record.ArchivedAt,
		Path:               record.Path,
		ClaimPath:          record.ClaimPath,
		DescriptionPreview: preview(record.Description),
	}
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

func resolveCurrentSessionID() string {
	for _, key := range []string{
		"AGENT_DASHBOARD_SESSION_ID",
		"CODEX_THREAD_ID",
		"AGENT_SESSION_ID",
	} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstAssignedTask(plan *Plan, sessionID string) *TaskRecord {
	for _, task := range plan.IterTasksInDisplayOrder() {
		if task.AssignedSessionID != sessionID {
			continue
		}
		if task.Status == StatusComplete || task.Status == StatusCancelled {
			continue
		}
		copy := task
		return &copy
	}
	return nil
}

func containsID(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
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
		"AGENT_DASHBOARD_SESSION_ID",
		"CODEX_THREAD_ID",
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
func (s *Store) sessionsDir() string          { return filepath.Join(s.MetaDir, sessionsDirName) }
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
func (s *Store) sessionBindingPath(sessionID string) string {
	return filepath.Join(s.sessionsDir(), sessionID+".json")
}
