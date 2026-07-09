package agentplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

func (s *Store) Stats(planID string) (PlanStats, error) {
	plan, manifest, err := s.loadPlan(planID)
	if err != nil {
		return PlanStats{}, err
	}
	return statsForPlan(plan, manifest), nil
}

func (s *Store) Audit(planID string) (AuditReport, error) {
	plan, manifest, err := s.loadPlan(planID)
	if err != nil {
		return AuditReport{}, err
	}
	stats := statsForPlan(plan, manifest)
	report := AuditReport{
		PlanID:   plan.ID,
		Stats:    stats,
		Findings: []AuditFinding{},
	}
	report.Findings = append(report.Findings, auditTaskGraph(plan)...)
	report.Findings = append(report.Findings, s.auditTaskFiles(plan, manifest)...)
	report.Findings = append(report.Findings, s.auditClaimFiles(plan)...)
	if lockStatus, err := s.PlanLockStatus(plan.ID); err == nil && lockStatus.Exists && lockStatus.OwnerAlive != nil && !*lockStatus.OwnerAlive {
		report.Findings = append(report.Findings, AuditFinding{
			Severity: "warning",
			Code:     "stale_lock",
			Message:  fmt.Sprintf("plan lock appears stale: %s", lockStatus.Path),
			Path:     lockStatus.Path,
		})
	}
	if stats.Total > 20 && stats.ReadyCount > stats.Total/2 {
		report.Findings = append(report.Findings, AuditFinding{
			Severity: "warning",
			Code:     "ready_count_high",
			Message:  fmt.Sprintf("%d of %d tasks are ready; dependency edges may be missing", stats.ReadyCount, stats.Total),
		})
	}
	sortAuditFindings(report.Findings)
	return report, nil
}

func (s *Store) PlanLockStatus(planID string) (LockStatus, error) {
	path := s.planLockPath(planID)
	status := LockStatus{
		PlanID:      planID,
		Path:        path,
		CurrentHost: os.Getenv("HOSTNAME"),
	}
	data, err := readFileFunc(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return LockStatus{}, err
	}
	status.Exists = true
	var payload struct {
		PID       int    `json:"pid"`
		Host      string `json:"host"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return status, nil
	}
	status.PID = payload.PID
	status.Host = payload.Host
	status.CreatedAt = payload.CreatedAt
	if createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt); err == nil {
		status.AgeSeconds = time.Since(createdAt).Seconds()
	}
	status.OwnerIsLocal = lockOwnerIsLocal(payload.Host, status.CurrentHost)
	if status.OwnerIsLocal && payload.PID > 0 {
		alive := processAlive(payload.PID)
		status.OwnerAlive = &alive
	}
	return status, nil
}

func (s *Store) ClearPlanLockIfDead(planID string) (LockClearResult, error) {
	status, err := s.PlanLockStatus(planID)
	if err != nil {
		return LockClearResult{}, err
	}
	result := LockClearResult{Status: status}
	if !status.Exists {
		return result, nil
	}
	if status.OwnerAlive == nil {
		return result, fmt.Errorf("refusing to clear lock without local owner liveness evidence")
	}
	if *status.OwnerAlive {
		return result, fmt.Errorf("refusing to clear lock because owner process %d appears alive", status.PID)
	}
	if err := removeFileFunc(status.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, err
	}
	status.Exists = false
	result.Cleared = true
	result.Status = status
	return result, nil
}

func statsForPlan(plan *Plan, manifest *Manifest) PlanStats {
	stats := PlanStats{
		PlanID:       plan.ID,
		Name:         plan.Name,
		Total:        len(plan.Tasks),
		StatusCounts: map[Status]int{},
		UpdatedAt:    plan.UpdatedAt,
	}
	if manifest != nil {
		stats.NextSequence = manifest.NextSequence
	}
	for _, task := range plan.Tasks {
		stats.StatusCounts[task.Status]++
		if task.ParentID == "" {
			stats.RootCount++
		} else {
			stats.ParentedCount++
		}
		if len(task.DependsOn) > 0 {
			stats.DependencyCount++
		}
		if task.ClaimedBy != "" {
			stats.ClaimedCount++
		}
		if task.Status == StatusComplete {
			stats.HistoryCount++
		}
	}
	stats.ReadyCount = len(plan.ReadyTasks())
	return stats
}

func auditTaskGraph(plan *Plan) []AuditFinding {
	findings := []AuditFinding{}
	names := map[string][]string{}
	importKeys := map[string][]string{}
	for _, task := range plan.Tasks {
		names[task.Name] = append(names[task.Name], task.ID)
		if key := strings.TrimSpace(task.ImportKey); key != "" {
			importKeys[key] = append(importKeys[key], task.ID)
		}
		if task.ParentID != "" && plan.GetTask(task.ParentID) == nil {
			findings = append(findings, AuditFinding{
				Severity: "error",
				Code:     "missing_parent",
				Message:  fmt.Sprintf("task parent does not exist: %s", task.ParentID),
				TaskID:   task.ID,
			})
		}
		for _, dependencyID := range task.DependsOn {
			if plan.GetTask(dependencyID) == nil {
				findings = append(findings, AuditFinding{
					Severity: "error",
					Code:     "missing_dependency",
					Message:  fmt.Sprintf("task dependency does not exist: %s", dependencyID),
					TaskID:   task.ID,
				})
				continue
			}
			if plan.TaskDependsOn(dependencyID, task.ID) {
				findings = append(findings, AuditFinding{
					Severity: "error",
					Code:     "dependency_cycle",
					Message:  fmt.Sprintf("dependency would create a cycle through %s", dependencyID),
					TaskID:   task.ID,
				})
			}
		}
	}
	for name, ids := range names {
		if strings.TrimSpace(name) == "" || len(ids) < 2 {
			continue
		}
		findings = append(findings, AuditFinding{
			Severity: "warning",
			Code:     "duplicate_task_name",
			Message:  fmt.Sprintf("task name %q is used by %s", name, strings.Join(ids, ", ")),
		})
	}
	for key, ids := range importKeys {
		if len(ids) < 2 {
			continue
		}
		findings = append(findings, AuditFinding{
			Severity: "error",
			Code:     "duplicate_import_key",
			Message:  fmt.Sprintf("import key %q is used by %s", key, strings.Join(ids, ", ")),
		})
	}
	return findings
}

func (s *Store) auditTaskFiles(plan *Plan, manifest *Manifest) []AuditFinding {
	if manifest == nil {
		return nil
	}
	knownPaths := map[string]struct{}{}
	for _, summary := range manifest.Tasks {
		if summary.Path != "" {
			knownPaths[filepath.Clean(summary.Path)] = struct{}{}
		}
	}
	findings := []AuditFinding{}
	for _, dir := range []string{s.planTasksDir(plan.ID), s.planHistoryDir(plan.ID)} {
		entries, err := readDirFunc(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			findings = append(findings, AuditFinding{Severity: "error", Code: "read_dir_failed", Message: err.Error(), Path: dir})
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".done") {
				continue
			}
			path := filepath.Join(dir, name)
			if _, ok := knownPaths[filepath.Clean(path)]; ok {
				continue
			}
			findings = append(findings, AuditFinding{
				Severity: "warning",
				Code:     "orphan_task_file",
				Message:  "task file is not referenced by the manifest",
				Path:     path,
			})
		}
	}
	return findings
}

func (s *Store) auditClaimFiles(plan *Plan) []AuditFinding {
	activeClaims := map[string]string{}
	for _, task := range plan.Tasks {
		if task.ClaimedBy != "" && task.Status != StatusComplete && task.Status != StatusCancelled && task.Status != StatusBlocked {
			activeClaims[filepath.Clean(s.claimPath(plan.ID, task.ID))] = task.ID
		}
	}
	entries, err := readDirFunc(s.planTasksDir(plan.ID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return []AuditFinding{{Severity: "error", Code: "read_dir_failed", Message: err.Error(), Path: s.planTasksDir(plan.ID)}}
	}
	findings := []AuditFinding{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".claim") {
			continue
		}
		path := filepath.Join(s.planTasksDir(plan.ID), entry.Name())
		if _, ok := activeClaims[filepath.Clean(path)]; ok {
			continue
		}
		findings = append(findings, AuditFinding{
			Severity: "warning",
			Code:     "orphan_claim_file",
			Message:  "claim file is not referenced by an active claimed task",
			Path:     path,
		})
	}
	return findings
}

func lockOwnerIsLocal(ownerHost, currentHost string) bool {
	ownerHost = strings.TrimSpace(ownerHost)
	currentHost = strings.TrimSpace(currentHost)
	return ownerHost == "" || currentHost == "" || ownerHost == currentHost
}

func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func sortAuditFindings(findings []AuditFinding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Severity != findings[j].Severity {
			return findings[i].Severity < findings[j].Severity
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].TaskID != findings[j].TaskID {
			return findings[i].TaskID < findings[j].TaskID
		}
		return findings[i].Path < findings[j].Path
	})
}
