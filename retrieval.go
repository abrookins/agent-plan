package agentplan

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultTaskQueryLimit = 25
	maxTaskQueryLimit     = 100
	taskPreviewRunes      = 240
	taskQueryByteBudget   = 64 * 1024
)

var defaultTaskQueryFields = []string{
	"id",
	"name",
	"status",
	"parent_id",
	"depends_on",
	"assignee",
	"claimed_by",
	"updated_at",
	"description_preview",
	"description_bytes",
	"result_preview",
	"result_bytes",
	"output_preview",
	"output_bytes",
}

var allowedTaskQueryFields = map[string]struct{}{
	"id":                  {},
	"sequence":            {},
	"import_key":          {},
	"name":                {},
	"status":              {},
	"order":               {},
	"parent_id":           {},
	"depends_on":          {},
	"assignee":            {},
	"blocked_reason":      {},
	"claimed_by":          {},
	"updated_at":          {},
	"description_preview": {},
	"description_bytes":   {},
	"result_preview":      {},
	"result_bytes":        {},
	"output_preview":      {},
	"output_bytes":        {},
	"description":         {},
	"result":              {},
	"output":              {},
	"path":                {},
	"claim_path":          {},
	"created_at":          {},
	"claimed_at":          {},
	"completed_at":        {},
	"archived_at":         {},
}

type TaskQuery struct {
	TaskRefs     []string
	Statuses     []Status
	ParentRef    string
	ClaimedBy    string
	Fields       []string
	Limit        int
	Cursor       string
	KnownVersion *int
}

type TaskQueryResult struct {
	PlanID     string           `json:"plan_id"`
	PlanName   string           `json:"plan_name,omitempty"`
	Version    int              `json:"version"`
	Unchanged  bool             `json:"unchanged"`
	Returned   int              `json:"returned,omitempty"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Fields     []string         `json:"fields,omitempty"`
	Tasks      []map[string]any `json:"tasks,omitempty"`
	Bytes      int              `json:"bytes,omitempty"`
	Omitted    int              `json:"-"`
}

type taskQueryCursor struct {
	PlanID    string `json:"plan_id"`
	Version   int    `json:"version"`
	Signature string `json:"signature"`
	Offset    int    `json:"offset"`
	LastID    string `json:"last_id,omitempty"`
}

func (s *Store) GetTasks(planID string, query TaskQuery) (TaskQueryResult, error) {
	record, err := s.loadPlanRecord(planID)
	if err != nil {
		return TaskQueryResult{}, err
	}
	if query.KnownVersion != nil && *query.KnownVersion == record.Version {
		return TaskQueryResult{
			PlanID:    record.ID,
			Version:   record.Version,
			Unchanged: true,
		}, nil
	}
	manifest, err := s.loadManifest(planID)
	if err != nil {
		return TaskQueryResult{}, err
	}
	normalized, err := normalizeTaskQuery(manifest, query)
	if err != nil {
		return TaskQueryResult{}, invalidArgumentError(err)
	}
	if len(normalized.TaskRefs) == 0 && len(normalized.Statuses) == 0 && normalized.ParentRef == "" && normalized.ClaimedBy == "" {
		return TaskQueryResult{}, invalidArgumentError(
			fmt.Errorf("task query requires at least one task reference or filter"),
		)
	}

	signature := taskQuerySignature(normalized)
	offset := 0
	var resumeCursor *taskQueryCursor
	if normalized.Cursor != "" {
		cursor, err := decodeTaskCursor(normalized.Cursor)
		if err != nil || cursor.PlanID != record.ID || cursor.Signature != signature {
			return TaskQueryResult{}, &PlanError{
				ErrorCode: ErrorCodeCursorStale,
				Message:   "task cursor does not match this query",
				PlanID:    record.ID,
				Err:       err,
			}
		}
		if cursor.Version != record.Version {
			return TaskQueryResult{}, &PlanError{
				ErrorCode: ErrorCodeCursorStale,
				Message:   "task cursor is stale",
				PlanID:    record.ID,
			}
		}
		offset = cursor.Offset
		resumeCursor = &cursor
	}

	selected, err := selectTaskSummaries(manifest, normalized)
	if err != nil {
		return TaskQueryResult{}, err
	}
	if offset < 0 || offset > len(selected) {
		return TaskQueryResult{}, &PlanError{
			ErrorCode: ErrorCodeCursorStale,
			Message:   "task cursor offset is stale",
			PlanID:    record.ID,
		}
	}
	if resumeCursor != nil {
		expectedLastID := ""
		if offset > 0 {
			expectedLastID = selected[offset-1].ID
		}
		if resumeCursor.LastID != expectedLastID {
			return TaskQueryResult{}, &PlanError{
				ErrorCode: ErrorCodeCursorStale,
				Message:   "task cursor ordering key is stale",
				PlanID:    record.ID,
			}
		}
	}

	result := TaskQueryResult{
		PlanID:    record.ID,
		PlanName:  unicodePreview(record.Name, taskPreviewRunes),
		Version:   record.Version,
		Unchanged: false,
		Fields:    append([]string(nil), normalized.Fields...),
		Tasks:     make([]map[string]any, 0, normalized.Limit),
	}
	nextOffset := offset
	for nextOffset < len(selected) && len(result.Tasks) < normalized.Limit {
		view, err := s.taskView(planID, selected[nextOffset], normalized.Fields)
		if err != nil {
			return TaskQueryResult{}, err
		}
		probe := result
		probe.Tasks = append(append([]map[string]any(nil), result.Tasks...), view)
		probe.Returned = len(probe.Tasks)
		probe.NextCursor = ""
		candidateOffset := nextOffset + 1
		if candidateOffset < len(selected) {
			probe.NextCursor, err = encodeTaskCursor(taskQueryCursor{
				PlanID:    record.ID,
				Version:   record.Version,
				Signature: signature,
				Offset:    candidateOffset,
				LastID:    selected[candidateOffset-1].ID,
			})
			if err != nil {
				return TaskQueryResult{}, err
			}
		}
		updateTaskQueryResultBytes(&probe)
		if probe.Bytes > taskQueryByteBudget && len(result.Tasks) > 0 {
			break
		}
		result = probe
		nextOffset = candidateOffset
	}
	if len(result.Tasks) == 0 {
		updateTaskQueryResultBytes(&result)
	}
	result.Omitted = len(selected) - offset - result.Returned
	return result, nil
}

func normalizeTaskQuery(manifest Manifest, query TaskQuery) (TaskQuery, error) {
	taskIDs := make([]string, 0, len(query.TaskRefs))
	for _, ref := range normalizeIDs(query.TaskRefs) {
		task, err := resolveTaskSummary(manifest.Tasks, ref)
		if err != nil {
			return TaskQuery{}, err
		}
		taskIDs = append(taskIDs, task.ID)
	}
	sort.Strings(taskIDs)
	query.TaskRefs = normalizeIDs(taskIDs)
	query.ClaimedBy = strings.TrimSpace(query.ClaimedBy)
	query.ParentRef = strings.TrimSpace(query.ParentRef)
	query.Cursor = strings.TrimSpace(query.Cursor)
	if query.Limit == 0 {
		query.Limit = defaultTaskQueryLimit
	}
	if query.Limit < 1 || query.Limit > maxTaskQueryLimit {
		return TaskQuery{}, fmt.Errorf("limit must be between 1 and %d", maxTaskQueryLimit)
	}
	if len(query.Fields) == 0 {
		query.Fields = append([]string(nil), defaultTaskQueryFields...)
	} else {
		seen := map[string]struct{}{}
		fields := make([]string, 0, len(query.Fields))
		for _, field := range query.Fields {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if _, ok := allowedTaskQueryFields[field]; !ok {
				return TaskQuery{}, fmt.Errorf("unknown task field %q", field)
			}
			if _, duplicate := seen[field]; duplicate {
				continue
			}
			seen[field] = struct{}{}
			fields = append(fields, field)
		}
		if len(fields) == 0 {
			return TaskQuery{}, fmt.Errorf("fields must include at least one supported field")
		}
		query.Fields = fields
	}
	statuses := make([]Status, 0, len(query.Statuses))
	seenStatuses := map[Status]struct{}{}
	for _, status := range query.Statuses {
		if !isValidStatus(status) {
			return TaskQuery{}, fmt.Errorf("invalid status: %s", status)
		}
		if _, duplicate := seenStatuses[status]; duplicate {
			continue
		}
		seenStatuses[status] = struct{}{}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i] < statuses[j] })
	query.Statuses = statuses
	if query.ParentRef != "" {
		parent, err := resolveTaskSummary(manifest.Tasks, query.ParentRef)
		if err != nil {
			return TaskQuery{}, err
		}
		query.ParentRef = parent.ID
	}
	return query, nil
}

func selectTaskSummaries(manifest Manifest, query TaskQuery) ([]TaskSummary, error) {
	refIDs := map[string]struct{}{}
	for _, ref := range query.TaskRefs {
		task, err := resolveTaskSummary(manifest.Tasks, ref)
		if err != nil {
			return nil, err
		}
		refIDs[task.ID] = struct{}{}
	}
	statuses := map[Status]struct{}{}
	for _, status := range query.Statuses {
		statuses[status] = struct{}{}
	}
	selected := make([]TaskSummary, 0)
	for _, task := range manifest.Tasks {
		if len(refIDs) > 0 {
			if _, ok := refIDs[task.ID]; !ok {
				continue
			}
		}
		if len(statuses) > 0 {
			if _, ok := statuses[task.Status]; !ok {
				continue
			}
		}
		if query.ParentRef != "" && task.ParentID != query.ParentRef {
			continue
		}
		if query.ClaimedBy != "" && task.ClaimedBy != query.ClaimedBy {
			continue
		}
		selected = append(selected, task)
	}
	sortSummaries(selected)
	return selected, nil
}

func resolveTaskSummary(tasks []TaskSummary, ref string) (*TaskSummary, error) {
	ref = strings.TrimSpace(ref)
	for i := range tasks {
		if tasks[i].ID == ref {
			task := tasks[i]
			return &task, nil
		}
	}
	matches := make([]TaskSummary, 0, 1)
	for _, task := range tasks {
		if task.Name == ref {
			matches = append(matches, task)
		}
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("multiple tasks matched %q", ref)
	}
	return nil, &PlanError{
		ErrorCode: ErrorCodeTaskNotFound,
		Message:   "task not found",
		TaskID:    ref,
	}
}

func (s *Store) taskView(planID string, summary TaskSummary, fields []string) (map[string]any, error) {
	needsRecord := false
	for _, field := range fields {
		switch field {
		case "description", "description_preview", "description_bytes", "result", "result_preview", "result_bytes", "output", "output_preview", "output_bytes":
			needsRecord = true
		}
	}
	record := TaskRecord{}
	if needsRecord {
		path, err := s.resolveManifestRecordPath(planID, summary.Path, false)
		if err != nil {
			return nil, err
		}
		record, err = s.readTaskRecord(path)
		if err != nil {
			code := ErrorCodePlanUnreadable
			message := "task record is unreadable"
			if strings.Contains(err.Error(), "no such file") {
				code = ErrorCodeTaskRecordMissing
				message = "task record missing"
			}
			return nil, &PlanError{
				ErrorCode: code,
				Message:   message,
				PlanID:    planID,
				TaskID:    summary.ID,
				Path:      summary.Path,
				Err:       err,
			}
		}
	}
	view := make(map[string]any, len(fields))
	for _, field := range fields {
		switch field {
		case "id":
			view[field] = summary.ID
		case "sequence":
			view[field] = summary.Sequence
		case "import_key":
			view[field] = summary.ImportKey
		case "name":
			view[field] = summary.Name
		case "status":
			view[field] = summary.Status
		case "order":
			view[field] = summary.Order
		case "parent_id":
			view[field] = emptyStringAsNil(summary.ParentID)
		case "depends_on":
			view[field] = append([]string(nil), summary.DependsOn...)
		case "assignee":
			view[field] = emptyStringAsNil(summary.Assignee)
		case "blocked_reason":
			view[field] = emptyStringAsNil(summary.BlockedReason)
		case "claimed_by":
			view[field] = emptyStringAsNil(summary.ClaimedBy)
		case "updated_at":
			view[field] = summary.UpdatedAt.UTC()
		case "description_preview":
			view[field] = unicodePreview(record.Description, taskPreviewRunes)
		case "description_bytes":
			view[field] = len([]byte(record.Description))
		case "result_preview":
			view[field] = unicodePreview(taskResult(record), taskPreviewRunes)
		case "result_bytes":
			view[field] = len([]byte(taskResult(record)))
		case "output_preview":
			view[field] = unicodePreview(record.Output, taskPreviewRunes)
		case "output_bytes":
			view[field] = len([]byte(record.Output))
		case "description":
			view[field] = record.Description
		case "result":
			view[field] = taskResult(record)
		case "output":
			view[field] = record.Output
		case "path":
			view[field] = summary.Path
		case "claim_path":
			view[field] = emptyStringAsNil(summary.ClaimPath)
		case "created_at":
			view[field] = summary.CreatedAt.UTC()
		case "claimed_at":
			view[field] = timePointerValue(summary.ClaimedAt)
		case "completed_at":
			view[field] = timePointerValue(summary.CompletedAt)
		case "archived_at":
			view[field] = timePointerValue(summary.ArchivedAt)
		}
	}
	return view, nil
}

func taskResult(task TaskRecord) string {
	if task.Result != "" {
		return task.Result
	}
	return task.Output
}

func taskQuerySignature(query TaskQuery) string {
	normalized := struct {
		TaskRefs  []string `json:"task_refs,omitempty"`
		Statuses  []Status `json:"statuses,omitempty"`
		ParentRef string   `json:"parent_ref,omitempty"`
		ClaimedBy string   `json:"claimed_by,omitempty"`
		Fields    []string `json:"fields"`
		Limit     int      `json:"limit"`
	}{
		TaskRefs:  query.TaskRefs,
		Statuses:  query.Statuses,
		ParentRef: query.ParentRef,
		ClaimedBy: query.ClaimedBy,
		Fields:    query.Fields,
		Limit:     query.Limit,
	}
	data, _ := json.Marshal(normalized)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func encodeTaskCursor(cursor taskQueryCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func decodeTaskCursor(encoded string) (taskQueryCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return taskQueryCursor{}, err
	}
	var cursor taskQueryCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return taskQueryCursor{}, err
	}
	return cursor, nil
}

func unicodePreview(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

func emptyStringAsNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func timePointerValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func encodedSize(value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return len(data)
}

func updateTaskQueryResultBytes(result *TaskQueryResult) {
	result.Bytes = 0
	for range 3 {
		size := encodedSize(*result)
		if size == result.Bytes {
			return
		}
		result.Bytes = size
	}
}
