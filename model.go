package agentplan

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func (p *Plan) GetTask(taskID string) *TaskRecord {
	for i := range p.Tasks {
		if p.Tasks[i].ID == taskID {
			return &p.Tasks[i]
		}
	}
	return nil
}

func (p *Plan) GetRootTasks() []TaskRecord {
	return p.getChildren("")
}

func (p *Plan) GetSubtasks(parentID string) []TaskRecord {
	return p.getChildren(parentID)
}

func (p *Plan) getChildren(parentID string) []TaskRecord {
	children := make([]TaskRecord, 0)
	for _, task := range p.Tasks {
		if task.ParentID == parentID {
			children = append(children, task)
		}
	}
	sortTasks(children)
	return children
}

func (p *Plan) IterTasksInDisplayOrder() []TaskRecord {
	ordered := make([]TaskRecord, 0, len(p.Tasks))
	var visit func(string)
	visit = func(parentID string) {
		for _, task := range p.getChildren(parentID) {
			ordered = append(ordered, task)
			visit(task.ID)
		}
	}
	visit("")
	return ordered
}

func (p *Plan) IncompleteDependencies(task TaskRecord) []TaskRecord {
	incomplete := make([]TaskRecord, 0, len(task.DependsOn))
	for _, dependencyID := range task.DependsOn {
		dependency := p.GetTask(dependencyID)
		if dependency == nil || dependency.Status != StatusComplete {
			if dependency != nil {
				incomplete = append(incomplete, *dependency)
			}
		}
	}
	return incomplete
}

func (p *Plan) IsTaskReady(task TaskRecord) bool {
	switch task.Status {
	case StatusComplete, StatusCancelled, StatusBlocked:
		return false
	}
	return len(p.IncompleteDependencies(task)) == 0
}

func (p *Plan) ReadyTasks() []TaskRecord {
	ready := make([]TaskRecord, 0)
	for _, task := range p.IterTasksInDisplayOrder() {
		if p.IsTaskReady(task) {
			ready = append(ready, task)
		}
	}
	return ready
}

func (p *Plan) SelectNextTask(sessionID string) *TaskRecord {
	ready := p.ReadyTasks()
	if len(ready) == 0 {
		return nil
	}
	if strings.TrimSpace(sessionID) == "" {
		task := ready[0]
		return &task
	}
	for _, task := range ready {
		if task.AssignedSessionID == sessionID {
			copy := task
			return &copy
		}
	}
	for _, task := range ready {
		if task.AssignedSessionID == "" {
			copy := task
			return &copy
		}
	}
	return nil
}

func (p *Plan) TaskDependsOn(taskID, targetID string) bool {
	visited := map[string]struct{}{}
	pending := []string{taskID}
	for len(pending) > 0 {
		currentID := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, ok := visited[currentID]; ok {
			continue
		}
		visited[currentID] = struct{}{}
		if currentID == targetID {
			return true
		}
		currentTask := p.GetTask(currentID)
		if currentTask != nil {
			pending = append(pending, currentTask.DependsOn...)
		}
	}
	return false
}

func (p *Plan) ValidateDependencyIDs(dependencyIDs []string, taskID string) ([]string, error) {
	normalized := make([]string, 0, len(dependencyIDs))
	seen := map[string]struct{}{}
	for _, dependencyID := range dependencyIDs {
		cleaned := strings.TrimSpace(dependencyID)
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		if taskID != "" && cleaned == taskID {
			return nil, fmt.Errorf("task cannot depend on itself")
		}
		dependencyTask := p.GetTask(cleaned)
		if dependencyTask == nil {
			return nil, fmt.Errorf("dependency task not found: %s", cleaned)
		}
		if taskID != "" && p.TaskDependsOn(cleaned, taskID) {
			return nil, fmt.Errorf("dependency would create a cycle")
		}
		seen[cleaned] = struct{}{}
		normalized = append(normalized, cleaned)
	}
	return normalized, nil
}

func (p *Plan) AddTask(name, description, parentID string, order *int, sequence int) (*TaskRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("task name is required")
	}
	if parentID != "" && p.GetTask(parentID) == nil {
		return nil, fmt.Errorf("parent task not found: %s", parentID)
	}

	siblings := p.getChildren(parentID)
	insertOrder := len(siblings)
	if order != nil {
		insertOrder = clampOrder(*order, len(siblings))
	}
	for i := range p.Tasks {
		if p.Tasks[i].ParentID == parentID && p.Tasks[i].Order >= insertOrder {
			p.Tasks[i].Order++
		}
	}

	now := time.Now().UTC()
	record := TaskRecord{
		ID:          fmt.Sprintf("%03d-%s", sequence, slugify(name)),
		Sequence:    sequence,
		Slug:        slugify(name),
		Name:        name,
		Status:      StatusNotStarted,
		Order:       insertOrder,
		ParentID:    parentID,
		CreatedAt:   now,
		UpdatedAt:   now,
		Description: strings.TrimSpace(description),
	}
	p.Tasks = append(p.Tasks, record)
	p.touch()
	return &p.Tasks[len(p.Tasks)-1], nil
}

func (p *Plan) SetTaskOrder(taskID string, order int) bool {
	task := p.GetTask(taskID)
	if task == nil {
		return false
	}
	siblings := make([]*TaskRecord, 0)
	for i := range p.Tasks {
		if p.Tasks[i].ParentID == task.ParentID && p.Tasks[i].ID != task.ID {
			siblings = append(siblings, &p.Tasks[i])
		}
	}
	sort.SliceStable(siblings, func(i, j int) bool {
		if siblings[i].Order != siblings[j].Order {
			return siblings[i].Order < siblings[j].Order
		}
		return siblings[i].Sequence < siblings[j].Sequence
	})
	insertOrder := clampOrder(order, len(siblings))
	ordered := make([]*TaskRecord, 0, len(siblings)+1)
	for i := 0; i < len(siblings)+1; i++ {
		if i == insertOrder {
			ordered = append(ordered, task)
		}
		if i < len(siblings) {
			ordered = append(ordered, siblings[i])
		}
	}
	for i, sibling := range ordered {
		sibling.Order = i
	}
	p.touch()
	return true
}

func (p *Plan) DeleteTask(taskID string) int {
	requested := map[string]struct{}{}
	if strings.TrimSpace(taskID) != "" {
		requested[strings.TrimSpace(taskID)] = struct{}{}
	}
	if len(requested) == 0 {
		return 0
	}
	original := map[string]TaskRecord{}
	for _, task := range p.Tasks {
		original[task.ID] = task
	}
	removed := map[string]struct{}{}
	for id := range requested {
		if _, ok := original[id]; ok {
			removed[id] = struct{}{}
		}
	}
	if len(removed) == 0 {
		return 0
	}

	changed := true
	for changed {
		changed = false
		for _, task := range p.Tasks {
			if _, ok := removed[task.ParentID]; ok {
				if _, exists := removed[task.ID]; !exists {
					removed[task.ID] = struct{}{}
					changed = true
				}
			}
		}
	}

	filtered := make([]TaskRecord, 0, len(p.Tasks))
	for _, task := range p.Tasks {
		if _, ok := removed[task.ID]; ok {
			continue
		}
		deps := make([]string, 0, len(task.DependsOn))
		for _, dep := range task.DependsOn {
			if _, ok := removed[dep]; !ok {
				deps = append(deps, dep)
			}
		}
		task.DependsOn = deps
		filtered = append(filtered, task)
	}
	p.Tasks = filtered
	p.normalizeSiblingOrder()
	p.touch()
	return len(removed)
}

func (p *Plan) ApplyPlanUpdate(opts PlanUpdateOptions) {
	if opts.Name != nil {
		p.Name = strings.TrimSpace(*opts.Name)
	}
	if opts.Description != nil {
		p.Description = *opts.Description
	}
	if opts.SpecPath != nil {
		p.SpecPath = strings.TrimSpace(*opts.SpecPath)
	}
	if opts.ClearSessions {
		p.SessionIDs = nil
	} else if opts.SessionIDs != nil {
		p.SessionIDs = normalizeIDs(opts.SessionIDs)
	}
	p.touch()
}

func (p *Plan) ApplyTaskUpdate(taskID string, opts TaskUpdateOptions) (*TaskRecord, error) {
	task := p.GetTask(taskID)
	if task == nil {
		return nil, ErrTaskNotFound
	}
	if opts.Status != nil {
		task.Status = *opts.Status
		now := time.Now().UTC()
		task.UpdatedAt = now
		if *opts.Status == StatusComplete {
			task.CompletedAt = &now
			task.ArchivedAt = &now
		} else {
			task.CompletedAt = nil
			if *opts.Status != StatusComplete {
				task.ArchivedAt = nil
			}
		}
	}
	if opts.Name != nil {
		name := strings.TrimSpace(*opts.Name)
		if name == "" {
			return nil, fmt.Errorf("task name is required")
		}
		task.Name = name
	}
	if opts.Description != nil {
		task.Description = *opts.Description
	}
	if opts.ClearAssignedSession {
		task.AssignedSessionID = ""
	} else if opts.AssignedSessionID != nil {
		task.AssignedSessionID = strings.TrimSpace(*opts.AssignedSessionID)
	}
	if opts.ClearDependencies {
		task.DependsOn = nil
	} else if opts.DependsOn != nil {
		deps, err := p.ValidateDependencyIDs(opts.DependsOn, task.ID)
		if err != nil {
			return nil, err
		}
		task.DependsOn = deps
	}
	if opts.Order != nil {
		if !p.SetTaskOrder(task.ID, *opts.Order) {
			return nil, ErrTaskNotFound
		}
		task = p.GetTask(task.ID)
	}
	task.UpdatedAt = time.Now().UTC()
	p.touch()
	return task, nil
}

func (p *Plan) touch() {
	p.UpdatedAt = time.Now().UTC()
	p.Version++
}

func (p *Plan) normalizeSiblingOrder() {
	parentIDs := map[string]struct{}{"": {}}
	for _, task := range p.Tasks {
		parentIDs[task.ParentID] = struct{}{}
	}
	for parentID := range parentIDs {
		siblings := make([]*TaskRecord, 0)
		for i := range p.Tasks {
			if p.Tasks[i].ParentID == parentID {
				siblings = append(siblings, &p.Tasks[i])
			}
		}
		sort.SliceStable(siblings, func(i, j int) bool {
			if siblings[i].Order != siblings[j].Order {
				return siblings[i].Order < siblings[j].Order
			}
			return siblings[i].Sequence < siblings[j].Sequence
		})
		for i, sibling := range siblings {
			sibling.Order = i
		}
	}
}

func sortTasks(tasks []TaskRecord) {
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Order != tasks[j].Order {
			return tasks[i].Order < tasks[j].Order
		}
		return tasks[i].Sequence < tasks[j].Sequence
	})
}

func clampOrder(order, length int) int {
	if order < 0 {
		return 0
	}
	if order > length {
		return length
	}
	return order
}
