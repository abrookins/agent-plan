package agentplan

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type BulkImportValidationError struct {
	Report BulkImportReport
}

func (e *BulkImportValidationError) Error() string {
	if len(e.Report.Errors) == 0 {
		return "bulk import validation failed"
	}
	return strings.Join(e.Report.Errors, "; ")
}

func (s *Store) ImportTasks(planID string, spec BulkImportSpec, opts BulkImportOptions) (*BulkImportReport, error) {
	var report *BulkImportReport
	err := s.withPlanLock(planID, func() error {
		plan, manifest, err := s.loadPlan(planID)
		if err != nil {
			return err
		}
		working, nextSequence, builtReport, err := buildBulkImportPlan(plan, manifest.NextSequence, spec, opts)
		report = &builtReport
		if err != nil {
			return err
		}
		if opts.DryRun {
			return nil
		}
		manifest.NextSequence = nextSequence
		if err := s.persistPlanLocked(working, manifest, nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		var validationErr *BulkImportValidationError
		if errors.As(err, &validationErr) {
			return &validationErr.Report, err
		}
		return report, err
	}
	return report, nil
}

func buildBulkImportPlan(plan *Plan, manifestNextSequence int, spec BulkImportSpec, opts BulkImportOptions) (*Plan, int, BulkImportReport, error) {
	report := BulkImportReport{
		PlanID: plan.ID,
		DryRun: opts.DryRun,
		Total:  len(spec.Tasks),
	}
	if len(spec.Tasks) == 0 {
		report.Errors = append(report.Errors, "bulk import requires at least one task")
		return nil, manifestNextSequence, report, &BulkImportValidationError{Report: report}
	}

	working := clonePlan(plan)
	nextSequence := manifestNextSequence
	if nextSequence == 0 {
		nextSequence = maxSequence(working.Tasks) + 1
	}

	original := map[string]TaskRecord{}
	for _, task := range working.Tasks {
		original[task.ID] = task
	}

	keyToID := map[string]string{}
	specTaskIDs := map[int]string{}
	seenKeys := map[string]int{}
	seenTargets := map[string]int{}
	errorsOut := []string{}

	importKeyIndex, duplicateImportKeys := importKeyIndex(working)
	for _, key := range duplicateImportKeys {
		errorsOut = append(errorsOut, fmt.Sprintf("existing tasks share import_key %q", key))
	}

	for index, taskSpec := range spec.Tasks {
		key := strings.TrimSpace(taskSpec.Key)
		name := strings.TrimSpace(taskSpec.Name)
		if key != "" {
			if previous, ok := seenKeys[key]; ok {
				errorsOut = append(errorsOut, fmt.Sprintf("task %d duplicates key %q from task %d", index, key, previous))
				continue
			}
			seenKeys[key] = index
		}
		if name == "" {
			errorsOut = append(errorsOut, fmt.Sprintf("task %d requires name", index))
			continue
		}
		if status := strings.TrimSpace(taskSpec.Status); status != "" && !isValidStatus(Status(status)) {
			errorsOut = append(errorsOut, fmt.Sprintf("task %q has invalid status %q", taskLabel(taskSpec, index), status))
			continue
		}

		target, err := resolveBulkTarget(working, importKeyIndex, taskSpec)
		if err != nil {
			errorsOut = append(errorsOut, err.Error())
			continue
		}
		created := false
		if target == nil {
			record, err := working.AddTask(name, bulkDescription(taskSpec), "", taskSpec.Order, nextSequence)
			if err != nil {
				errorsOut = append(errorsOut, err.Error())
				continue
			}
			nextSequence++
			target = record
			created = true
		}
		if _, exists := seenTargets[target.ID]; exists {
			errorsOut = append(errorsOut, fmt.Sprintf("multiple import tasks target %s", target.ID))
			continue
		}
		seenTargets[target.ID] = index
		if key != "" {
			keyToID[key] = target.ID
			target.ImportKey = key
			importKeyIndex[key] = target.ID
		}
		if id := strings.TrimSpace(taskSpec.ID); id != "" {
			keyToID[id] = target.ID
		}
		keyToID[target.ID] = target.ID
		specTaskIDs[index] = target.ID
		if created {
			report.Changes = append(report.Changes, BulkImportChange{Action: "create", Key: key, ID: target.ID, Name: name})
		}
	}

	if len(errorsOut) == 0 {
		for index, taskSpec := range spec.Tasks {
			taskID, ok := specTaskIDs[index]
			if !ok {
				continue
			}
			if err := applyBulkTaskSpec(working, keyToID, taskID, taskSpec); err != nil {
				errorsOut = append(errorsOut, err.Error())
			}
		}
	}
	if len(errorsOut) == 0 {
		for index, chain := range spec.Chains {
			if err := applyBulkChain(working, keyToID, chain); err != nil {
				errorsOut = append(errorsOut, fmt.Sprintf("chain %d: %s", index, err))
			}
		}
	}
	if len(errorsOut) == 0 {
		for _, task := range working.Tasks {
			current := working.GetTask(task.ID)
			if current == nil {
				continue
			}
			deps, err := working.ValidateDependencyIDs(current.DependsOn, current.ID)
			if err != nil {
				errorsOut = append(errorsOut, fmt.Sprintf("task %s: %s", current.ID, err))
				continue
			}
			current.DependsOn = deps
		}
	}

	if len(errorsOut) > 0 {
		sort.Strings(errorsOut)
		report.Errors = errorsOut
		return nil, manifestNextSequence, report, &BulkImportValidationError{Report: report}
	}

	rebuildBulkChanges(&report, original, working, seenTargets)
	return working, nextSequence, report, nil
}

func clonePlan(plan *Plan) *Plan {
	copyPlan := *plan
	copyPlan.Tasks = append([]TaskRecord(nil), plan.Tasks...)
	return &copyPlan
}

func importKeyIndex(plan *Plan) (map[string]string, []string) {
	index := map[string]string{}
	duplicates := map[string]struct{}{}
	for _, task := range plan.Tasks {
		key := strings.TrimSpace(task.ImportKey)
		if key == "" {
			continue
		}
		if _, exists := index[key]; exists {
			duplicates[key] = struct{}{}
			continue
		}
		index[key] = task.ID
	}
	duplicateList := make([]string, 0, len(duplicates))
	for key := range duplicates {
		duplicateList = append(duplicateList, key)
	}
	sort.Strings(duplicateList)
	return index, duplicateList
}

func resolveBulkTarget(plan *Plan, importKeys map[string]string, spec BulkImportTaskSpec) (*TaskRecord, error) {
	key := strings.TrimSpace(spec.Key)
	if key != "" {
		if id := importKeys[key]; id != "" {
			return plan.GetTask(id), nil
		}
	}
	if id := strings.TrimSpace(spec.ID); id != "" {
		task := plan.GetTask(id)
		if task == nil {
			return nil, fmt.Errorf("task id %q was not found", id)
		}
		return task, nil
	}
	name := strings.TrimSpace(spec.Name)
	matches := make([]*TaskRecord, 0, 1)
	for i := range plan.Tasks {
		if plan.Tasks[i].Name == name {
			matches = append(matches, &plan.Tasks[i])
		}
	}
	if len(matches) > 1 {
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		return nil, fmt.Errorf("task name %q matches multiple existing tasks: %s", name, strings.Join(ids, ", "))
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return nil, nil
}

func applyBulkTaskSpec(plan *Plan, refs map[string]string, taskID string, spec BulkImportTaskSpec) error {
	task := plan.GetTask(taskID)
	if task == nil {
		return ErrTaskNotFound
	}
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		return fmt.Errorf("task %s requires name", taskID)
	}
	task.Name = name
	task.Slug = slugify(name)
	if spec.Description != nil {
		task.Description = strings.TrimSpace(*spec.Description)
	}
	if key := strings.TrimSpace(spec.Key); key != "" {
		task.ImportKey = key
	}
	if spec.Status != "" {
		applyBulkStatus(task, Status(strings.TrimSpace(spec.Status)))
	}
	if spec.ClaimedBy != nil {
		claimer := strings.TrimSpace(*spec.ClaimedBy)
		task.ClaimedBy = claimer
		task.Assignee = claimer
		if claimer == "" {
			task.ClaimedAt = nil
		} else if task.ClaimedAt == nil {
			now := time.Now().UTC()
			task.ClaimedAt = &now
		}
	}
	if spec.Parent != nil {
		parentID := ""
		if cleaned := strings.TrimSpace(*spec.Parent); cleaned != "" {
			resolved, err := resolveBulkRef(plan, refs, cleaned, "parent task")
			if err != nil {
				return err
			}
			parentID = resolved
		}
		if _, err := plan.ApplyTaskUpdate(taskID, TaskUpdateOptions{ParentID: &parentID, Order: spec.Order}); err != nil {
			return err
		}
	} else if spec.Order != nil {
		if _, err := plan.ApplyTaskUpdate(taskID, TaskUpdateOptions{Order: spec.Order}); err != nil {
			return err
		}
	}
	if spec.DependsOn != nil {
		deps := make([]string, 0, len(spec.DependsOn))
		for _, ref := range spec.DependsOn {
			resolved, err := resolveBulkRef(plan, refs, ref, "dependency task")
			if err != nil {
				return err
			}
			deps = append(deps, resolved)
		}
		normalized, err := plan.ValidateDependencyIDs(deps, taskID)
		if err != nil {
			return err
		}
		task = plan.GetTask(taskID)
		if task == nil {
			return ErrTaskNotFound
		}
		task.DependsOn = normalized
	}
	task = plan.GetTask(taskID)
	if task != nil {
		task.UpdatedAt = time.Now().UTC()
	}
	plan.touch()
	return nil
}

func applyBulkStatus(task *TaskRecord, status Status) {
	task.Status = status
	now := time.Now().UTC()
	task.UpdatedAt = now
	if status == StatusComplete {
		task.CompletedAt = &now
		task.ArchivedAt = &now
		task.ClaimedBy = ""
		task.Assignee = ""
		task.ClaimedAt = nil
		return
	}
	task.CompletedAt = nil
	task.ArchivedAt = nil
	if status == StatusCancelled || status == StatusBlocked {
		task.ClaimedBy = ""
		task.Assignee = ""
		task.ClaimedAt = nil
	}
}

func applyBulkChain(plan *Plan, refs map[string]string, chain []string) error {
	if len(chain) < 2 {
		return nil
	}
	for i := 1; i < len(chain); i++ {
		previousID, err := resolveBulkRef(plan, refs, chain[i-1], "chain dependency task")
		if err != nil {
			return err
		}
		currentID, err := resolveBulkRef(plan, refs, chain[i], "chain task")
		if err != nil {
			return err
		}
		current := plan.GetTask(currentID)
		if current == nil {
			return ErrTaskNotFound
		}
		deps := append([]string(nil), current.DependsOn...)
		deps = append(deps, previousID)
		normalized, err := plan.ValidateDependencyIDs(deps, current.ID)
		if err != nil {
			return err
		}
		current.DependsOn = normalized
		current.UpdatedAt = time.Now().UTC()
	}
	plan.touch()
	return nil
}

func resolveBulkRef(plan *Plan, refs map[string]string, ref, label string) (string, error) {
	cleaned := strings.TrimSpace(ref)
	if cleaned == "" {
		return "", fmt.Errorf("%s reference is required", label)
	}
	if id := refs[cleaned]; id != "" {
		return id, nil
	}
	if task := plan.GetTask(cleaned); task != nil {
		return task.ID, nil
	}
	matches := make([]string, 0, 1)
	for _, task := range plan.Tasks {
		if task.Name == cleaned {
			matches = append(matches, task.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("%s reference %q matches multiple tasks: %s", label, cleaned, strings.Join(matches, ", "))
	}
	return "", fmt.Errorf("%s reference not found: %s", label, cleaned)
}

func bulkDescription(spec BulkImportTaskSpec) string {
	if spec.Description == nil {
		return ""
	}
	return strings.TrimSpace(*spec.Description)
}

func taskLabel(spec BulkImportTaskSpec, index int) string {
	if key := strings.TrimSpace(spec.Key); key != "" {
		return key
	}
	if id := strings.TrimSpace(spec.ID); id != "" {
		return id
	}
	if name := strings.TrimSpace(spec.Name); name != "" {
		return name
	}
	return fmt.Sprintf("%d", index)
}

func rebuildBulkChanges(report *BulkImportReport, original map[string]TaskRecord, plan *Plan, touched map[string]int) {
	changes := make([]BulkImportChange, 0, len(touched))
	for taskID := range touched {
		task := plan.GetTask(taskID)
		if task == nil {
			continue
		}
		action := "unchanged"
		if previous, existed := original[taskID]; !existed {
			action = "create"
		} else if !bulkTaskEqual(previous, *task) {
			action = "update"
		}
		changes = append(changes, BulkImportChange{
			Action: action,
			Key:    task.ImportKey,
			ID:     task.ID,
			Name:   task.Name,
		})
	}
	sort.SliceStable(changes, func(i, j int) bool {
		return changes[i].ID < changes[j].ID
	})
	report.Changes = changes
	for _, change := range changes {
		switch change.Action {
		case "create":
			report.Created++
		case "update":
			report.Updated++
		default:
			report.Unchanged++
		}
	}
}

func bulkTaskEqual(a, b TaskRecord) bool {
	return a.ImportKey == b.ImportKey &&
		a.Name == b.Name &&
		a.Description == b.Description &&
		a.ParentID == b.ParentID &&
		a.Status == b.Status &&
		a.Order == b.Order &&
		a.ClaimedBy == b.ClaimedBy &&
		a.Assignee == b.Assignee &&
		stringSlicesEqual(normalizeIDs(a.DependsOn), normalizeIDs(b.DependsOn))
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
