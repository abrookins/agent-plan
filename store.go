package agentplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	metaDirName      = ".agent-plan"
	tasksDirName     = "tasks"
	historyDirName   = "history"
	manifestFileName = "manifest.json"
	manifestLockName = "manifest.lock"
	defaultTaskPerm  = 0o644
	defaultDirPerm   = 0o755
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
	renameFunc            = os.Rename
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
	if err := mkdirAllFunc(store.tasksDir(), defaultDirPerm); err != nil {
		return nil, err
	}
	if err := mkdirAllFunc(store.historyDir(), defaultDirPerm); err != nil {
		return nil, err
	}

	if _, err := statFunc(store.manifestPath()); err == nil {
		return store, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	now := time.Now().UTC()
	manifest := Manifest{
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
		NextSequence: 1,
		Tasks:        []TaskSummary{},
	}
	if err := store.writeManifest(manifest); err != nil {
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
	if _, err := statFunc(store.manifestPath()); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrPlanNotInitialized
		}
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

func (s *Store) Add(title string, opts AddOptions) (*TaskRecord, error) {
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("title is required")
	}

	var created *TaskRecord
	err := s.withManifestLock(func() error {
		manifest, err := s.loadManifest()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		sequence := manifest.NextSequence
		slug := slugify(title)
		id := fmt.Sprintf("%03d-%s", sequence, slug)

		order := s.nextOrder(manifest)
		if opts.Order != nil && *opts.Order >= 0 {
			order = *opts.Order
		}

		record := &TaskRecord{
			ID:          id,
			Sequence:    sequence,
			Slug:        slug,
			Title:       strings.TrimSpace(title),
			Status:      StatusNotStarted,
			Order:       order,
			DependsOn:   normalizeIDs(opts.DependsOn),
			CreatedAt:   now,
			UpdatedAt:   now,
			Description: strings.TrimSpace(opts.Description),
			Path:        s.taskPath(id),
			ClaimPath:   s.claimPath(id),
		}
		if err := s.validateDependencies(manifest, record.DependsOn); err != nil {
			return err
		}
		if err := s.writeTaskRecord(record.Path, *record); err != nil {
			return err
		}

		summary := summaryFromRecord(*record)
		manifest.Tasks = append(manifest.Tasks, summary)
		manifest.NextSequence = sequence + 1
		manifest.UpdatedAt = now
		sortSummaries(manifest.Tasks)
		if err := s.writeManifest(manifest); err != nil {
			return err
		}
		created = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Store) List(opts ListOptions) ([]TaskSummary, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	tasks := make([]TaskSummary, 0, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		hydrated, err := s.hydrateSummary(task)
		if err != nil {
			return nil, err
		}
		if opts.Status != nil && hydrated.Status != *opts.Status {
			continue
		}
		tasks = append(tasks, hydrated)
	}
	sortSummaries(tasks)
	return tasks, nil
}

func (s *Store) Next() (*TaskSummary, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	return s.nextFromManifest(manifest)
}

func (s *Store) Claim(id, by string) (*TaskRecord, error) {
	by = strings.TrimSpace(by)
	if by == "" {
		by = defaultActor()
	}

	var claimed *TaskRecord
	err := s.withManifestLock(func() error {
		manifest, err := s.loadManifest()
		if err != nil {
			return err
		}
		index := findSummaryIndex(manifest.Tasks, id)
		if index < 0 {
			return ErrTaskNotFound
		}

		summary, err := s.hydrateSummary(manifest.Tasks[index])
		if err != nil {
			return err
		}
		if summary.Status == StatusComplete || summary.Status == StatusCancelled {
			return fmt.Errorf("task %s is not claimable from status %s", id, summary.Status)
		}
		if summary.ClaimedBy != "" {
			return ErrTaskAlreadyClaimed
		}

		now := time.Now().UTC()
		claim := ClaimInfo{
			By:        by,
			ClaimedAt: now,
			Host:      os.Getenv("HOSTNAME"),
			PID:       os.Getpid(),
		}
		if err := s.writeExclusiveJSON(summary.ClaimPath, claim); err != nil {
			if errors.Is(err, os.ErrExist) {
				return ErrTaskAlreadyClaimed
			}
			return err
		}

		record, err := s.readTaskRecord(summary.Path)
		if err != nil {
			_ = removeFileFunc(summary.ClaimPath)
			return err
		}
		record.ClaimedBy = by
		record.ClaimedAt = &now
		record.Assignee = by
		record.Status = StatusInProgress
		record.UpdatedAt = now
		if err := s.writeTaskRecord(record.Path, record); err != nil {
			_ = removeFileFunc(summary.ClaimPath)
			return err
		}

		updated := summaryFromRecord(record)
		manifest.Tasks[index] = updated
		manifest.UpdatedAt = now
		sortSummaries(manifest.Tasks)
		if err := s.writeManifest(manifest); err != nil {
			_ = removeFileFunc(summary.ClaimPath)
			return err
		}
		claimed = &record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (s *Store) Done(id, output, by string) (*TaskRecord, error) {
	output = strings.TrimSpace(output)
	by = strings.TrimSpace(by)

	var completed *TaskRecord
	err := s.withManifestLock(func() error {
		manifest, err := s.loadManifest()
		if err != nil {
			return err
		}
		index := findSummaryIndex(manifest.Tasks, id)
		if index < 0 {
			return ErrTaskNotFound
		}

		summary, err := s.hydrateSummary(manifest.Tasks[index])
		if err != nil {
			return err
		}
		record, err := s.readTaskRecord(summary.Path)
		if err != nil {
			return err
		}

		if summary.ClaimedBy != "" && by != "" && summary.ClaimedBy != by {
			return fmt.Errorf("task %s is claimed by %s", id, summary.ClaimedBy)
		}
		now := time.Now().UTC()
		if by == "" {
			if summary.ClaimedBy != "" {
				by = summary.ClaimedBy
			} else {
				by = defaultActor()
			}
		}

		record.Assignee = by
		record.ClaimedBy = ""
		record.ClaimedAt = nil
		record.Status = StatusComplete
		record.Output = output
		record.CompletedAt = &now
		record.ArchivedAt = &now
		record.UpdatedAt = now
		activePath := record.Path
		historyPath := s.historyPath(id)
		record.Path = historyPath
		record.ClaimPath = ""

		if err := s.writeTaskRecord(historyPath, record); err != nil {
			return err
		}
		if err := removeFileFunc(activePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if summary.ClaimPath != "" {
			if err := removeFileFunc(summary.ClaimPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}

		updated := summaryFromRecord(record)
		manifest.Tasks[index] = updated
		manifest.UpdatedAt = now
		sortSummaries(manifest.Tasks)
		if err := s.writeManifest(manifest); err != nil {
			return err
		}
		completed = &record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return completed, nil
}

func (s *Store) Block(id, reason, by string) (*TaskRecord, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}
	by = strings.TrimSpace(by)

	var blocked *TaskRecord
	err := s.withManifestLock(func() error {
		manifest, err := s.loadManifest()
		if err != nil {
			return err
		}
		index := findSummaryIndex(manifest.Tasks, id)
		if index < 0 {
			return ErrTaskNotFound
		}
		summary, err := s.hydrateSummary(manifest.Tasks[index])
		if err != nil {
			return err
		}
		record, err := s.readTaskRecord(summary.Path)
		if err != nil {
			return err
		}

		if summary.ClaimedBy != "" && by != "" && summary.ClaimedBy != by {
			return fmt.Errorf("task %s is claimed by %s", id, summary.ClaimedBy)
		}
		now := time.Now().UTC()
		if by == "" && summary.ClaimedBy != "" {
			by = summary.ClaimedBy
		}
		if by != "" {
			record.Assignee = by
		}
		record.Status = StatusBlocked
		record.BlockedReason = reason
		record.ClaimedBy = ""
		record.ClaimedAt = nil
		record.UpdatedAt = now
		if err := s.writeTaskRecord(record.Path, record); err != nil {
			return err
		}
		if summary.ClaimPath != "" {
			if err := removeFileFunc(summary.ClaimPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		updated := summaryFromRecord(record)
		manifest.Tasks[index] = updated
		manifest.UpdatedAt = now
		sortSummaries(manifest.Tasks)
		if err := s.writeManifest(manifest); err != nil {
			return err
		}
		blocked = &record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return blocked, nil
}

func (s *Store) Overview() (*Overview, error) {
	manifest, err := s.loadManifest()
	if err != nil {
		return nil, err
	}
	tasks := make([]TaskSummary, 0, len(manifest.Tasks))
	counts := map[string]int{}
	for _, task := range manifest.Tasks {
		hydrated, err := s.hydrateSummary(task)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, hydrated)
		counts[string(hydrated.Status)]++
	}
	manifest.Tasks = tasks
	sortSummaries(manifest.Tasks)
	next, err := s.nextFromManifest(manifest)
	if err != nil {
		return nil, err
	}
	return &Overview{
		Manifest: manifest,
		Counts:   counts,
		Next:     next,
	}, nil
}

func (s *Store) manifestPath() string { return filepath.Join(s.MetaDir, manifestFileName) }
func (s *Store) lockPath() string     { return filepath.Join(s.MetaDir, manifestLockName) }
func (s *Store) tasksDir() string     { return filepath.Join(s.MetaDir, tasksDirName) }
func (s *Store) historyDir() string   { return filepath.Join(s.MetaDir, historyDirName) }
func (s *Store) taskPath(id string) string {
	return filepath.Join(s.tasksDir(), id+".md")
}
func (s *Store) claimPath(id string) string {
	return filepath.Join(s.tasksDir(), id+".claim")
}
func (s *Store) historyPath(id string) string {
	return filepath.Join(s.historyDir(), id+".done")
}

func (s *Store) nextOrder(manifest Manifest) int {
	maxOrder := -1
	for _, task := range manifest.Tasks {
		if task.Order > maxOrder {
			maxOrder = task.Order
		}
	}
	return maxOrder + 1
}

func (s *Store) nextFromManifest(manifest Manifest) (*TaskSummary, error) {
	tasks := make([]TaskSummary, 0, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		hydrated, err := s.hydrateSummary(task)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, hydrated)
	}
	sortSummaries(tasks)
	taskByID := map[string]TaskSummary{}
	for _, task := range tasks {
		taskByID[task.ID] = task
	}
	for _, task := range tasks {
		if task.Status == StatusBlocked || task.Status == StatusComplete || task.Status == StatusCancelled {
			continue
		}
		if task.ClaimedBy != "" {
			continue
		}
		if dependenciesReady(task, taskByID) {
			candidate := task
			return &candidate, nil
		}
	}
	return nil, nil
}

func (s *Store) validateDependencies(manifest Manifest, ids []string) error {
	taskByID := map[string]TaskSummary{}
	for _, task := range manifest.Tasks {
		taskByID[task.ID] = task
	}
	for _, id := range ids {
		if _, ok := taskByID[id]; !ok {
			return fmt.Errorf("dependency task not found: %s", id)
		}
	}
	return nil
}

func dependenciesReady(task TaskSummary, taskByID map[string]TaskSummary) bool {
	for _, depID := range task.DependsOn {
		dep, ok := taskByID[depID]
		if !ok || dep.Status != StatusComplete {
			return false
		}
	}
	return true
}

func (s *Store) loadManifest() (Manifest, error) {
	data, err := readFileFunc(s.manifestPath())
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

func (s *Store) writeManifest(manifest Manifest) error {
	if err := mkdirAllFunc(s.MetaDir, defaultDirPerm); err != nil {
		return err
	}
	data, err := jsonMarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.manifestPath(), data, defaultTaskPerm)
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
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	id = strings.TrimSuffix(id, ".done")
	record.ClaimPath = s.claimPath(record.ID)
	if record.ID == "" {
		record.ID = id
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
	return atomicWriteFile(path, data, defaultTaskPerm)
}

func (s *Store) hydrateSummary(summary TaskSummary) (TaskSummary, error) {
	if summary.Path == "" {
		if summary.Status == StatusComplete {
			summary.Path = s.historyPath(summary.ID)
		} else {
			summary.Path = s.taskPath(summary.ID)
		}
	}
	if summary.Status != StatusComplete && summary.Status != StatusCancelled {
		summary.ClaimPath = s.claimPath(summary.ID)
		if data, err := readFileFunc(summary.ClaimPath); err == nil {
			var claim ClaimInfo
			if err := json.Unmarshal(data, &claim); err == nil {
				summary.ClaimedBy = claim.By
				summary.Assignee = claim.By
				claimedAt := claim.ClaimedAt.UTC()
				summary.ClaimedAt = &claimedAt
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return TaskSummary{}, err
		} else {
			summary.ClaimedBy = ""
			summary.ClaimedAt = nil
		}
	}
	return summary, nil
}

func summaryFromRecord(record TaskRecord) TaskSummary {
	return TaskSummary{
		ID:                 record.ID,
		Sequence:           record.Sequence,
		Title:              record.Title,
		Status:             record.Status,
		Order:              record.Order,
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

	var record TaskRecord
	if err := yamlUnmarshalFunc([]byte(frontmatter), &record); err != nil {
		return TaskRecord{}, err
	}
	record.Description = strings.TrimSpace(body)
	return record, nil
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

func (s *Store) withManifestLock(fn func() error) error {
	if err := mkdirAllFunc(s.MetaDir, defaultDirPerm); err != nil {
		return err
	}
	deadline := time.Now().Add(lockWaitTimeout)
	lockPayload := map[string]any{
		"pid":        os.Getpid(),
		"host":       os.Getenv("HOSTNAME"),
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	for {
		err := s.writeExclusiveJSON(s.lockPath(), lockPayload)
		if err == nil {
			defer func() { _ = removeFileFunc(s.lockPath()) }()
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for manifest lock")
		}
		time.Sleep(lockPollInterval)
	}
}

func (s *Store) writeExclusiveJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeExclusiveFile(path, data, defaultTaskPerm)
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
		if _, err := statFunc(filepath.Join(current, metaDirName)); err == nil {
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
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
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

func findSummaryIndex(tasks []TaskSummary, id string) int {
	for i, task := range tasks {
		if task.ID == id {
			return i
		}
	}
	return -1
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
		"AGENT_TODO_ASSIGNEE",
		"AGENT_NAME",
		"AGENT_DASHBOARD_SESSION_ID",
		"CODEX_THREAD_ID",
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
