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
	record, err := s.loadPlanRecord(planID)
	if err != nil {
		return PlanStats{}, err
	}
	manifest, err := s.loadManifest(planID)
	if err != nil {
		return PlanStats{
			PlanID:       record.ID,
			Name:         unicodePreview(record.Name, taskPreviewRunes),
			Version:      record.Version,
			Health:       "unreadable",
			FindingCount: 1,
			StatusCounts: map[Status]int{},
			UpdatedAt:    record.UpdatedAt.UTC(),
		}, nil
	}
	stats := statsForManifest(record, manifest)
	for _, task := range manifest.Tasks {
		path, err := s.resolveManifestRecordPath(planID, task.Path, false)
		if err != nil {
			stats.FindingCount++
			continue
		}
		if _, err := statFunc(path); err != nil {
			stats.FindingCount++
		}
		if taskSummaryHasActiveClaim(task) {
			claimPath, err := s.resolveManifestRecordPath(planID, task.ClaimPath, true)
			if err != nil {
				stats.FindingCount++
			} else if _, err := statFunc(claimPath); err != nil {
				stats.FindingCount++
			}
		}
	}
	if stats.FindingCount > 0 {
		stats.Health = "degraded"
	}
	return stats, nil
}

func (s *Store) Audit(planID string) (AuditReport, error) {
	record, err := s.loadPlanRecord(planID)
	if err != nil {
		return AuditReport{}, err
	}
	manifest, err := s.loadManifest(planID)
	if err != nil {
		finding := AuditFinding{
			Severity: "error",
			Code:     errorObject(err).Code,
			Message:  errorObject(err).Message,
			Path:     errorObject(err).Path,
		}
		return AuditReport{
			PlanID: planID,
			Stats: PlanStats{
				PlanID:       record.ID,
				Name:         record.Name,
				Version:      record.Version,
				Health:       "unreadable",
				FindingCount: 1,
				StatusCounts: map[Status]int{},
				UpdatedAt:    record.UpdatedAt.UTC(),
			},
			Findings: []AuditFinding{finding},
		}, nil
	}
	plan := &Plan{
		ID:          record.ID,
		Name:        record.Name,
		Description: record.Description,
		SpecPath:    record.SpecPath,
		CreatedAt:   record.CreatedAt.UTC(),
		UpdatedAt:   record.UpdatedAt.UTC(),
		Version:     record.Version,
		Tasks:       make([]TaskRecord, 0, len(manifest.Tasks)),
	}
	stats := statsForManifest(record, manifest)
	report := AuditReport{
		PlanID:   plan.ID,
		Stats:    stats,
		Findings: []AuditFinding{},
	}
	knownTaskPaths := make(map[string]string, len(manifest.Tasks))
	knownClaimPaths := make(map[string]TaskSummary, len(manifest.Tasks))
	for _, summary := range manifest.Tasks {
		recordPath, err := s.resolveManifestRecordPath(planID, summary.Path, false)
		if err != nil {
			report.Findings = append(report.Findings, AuditFinding{
				Severity: "error",
				Code:     ErrorCodePathOutsidePlan,
				Message:  err.Error(),
				TaskID:   summary.ID,
				Path:     summary.Path,
			})
			continue
		}
		cleanedPath := filepath.Clean(recordPath)
		if previousTaskID, duplicate := knownTaskPaths[cleanedPath]; duplicate {
			report.Findings = append(report.Findings, AuditFinding{
				Severity: "error",
				Code:     "duplicate_record_path",
				Message:  fmt.Sprintf("task record path is also used by %s", previousTaskID),
				TaskID:   summary.ID,
				Path:     summary.Path,
			})
		} else {
			knownTaskPaths[cleanedPath] = summary.ID
		}
		task, err := s.readTaskRecord(recordPath)
		if err != nil {
			code := "task_record_unreadable"
			message := "task record is unreadable"
			if errors.Is(err, os.ErrNotExist) {
				code = "missing_task_record"
				message = "task record is missing"
			}
			report.Findings = append(report.Findings, AuditFinding{
				Severity: "error",
				Code:     code,
				Message:  message,
				TaskID:   summary.ID,
				Path:     summary.Path,
			})
			continue
		}
		task.Path = summary.Path
		task.ClaimPath = summary.ClaimPath
		plan.Tasks = append(plan.Tasks, task)
		if taskSummaryHasActiveClaim(summary) {
			claimPath, err := s.resolveManifestRecordPath(planID, summary.ClaimPath, true)
			if err != nil {
				report.Findings = append(report.Findings, AuditFinding{
					Severity: "error",
					Code:     ErrorCodePathOutsidePlan,
					Message:  err.Error(),
					TaskID:   summary.ID,
					Path:     summary.ClaimPath,
				})
			} else {
				knownClaimPaths[filepath.Clean(claimPath)] = summary
				data, err := readFileFunc(claimPath)
				if err != nil {
					report.Findings = append(report.Findings, AuditFinding{
						Severity: "error",
						Code:     "missing_claim_record",
						Message:  "active task claim is missing",
						TaskID:   summary.ID,
						Path:     summary.ClaimPath,
					})
				} else {
					var claim ClaimInfo
					if json.Unmarshal(data, &claim) != nil || claim.By != summary.ClaimedBy {
						report.Findings = append(report.Findings, AuditFinding{
							Severity: "error",
							Code:     "claim_state_mismatch",
							Message:  "claim record disagrees with manifest state",
							TaskID:   summary.ID,
							Path:     summary.ClaimPath,
						})
					}
				}
			}
		}
	}
	report.Findings = append(report.Findings, auditTaskGraph(plan)...)
	report.Findings = append(report.Findings, s.auditOrphanTaskFiles(planID, knownTaskPaths)...)
	report.Findings = append(report.Findings, s.auditOrphanClaimFiles(planID, knownClaimPaths)...)
	storageFindingCount := len(report.Findings)
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
	report.Stats.FindingCount = storageFindingCount
	if storageFindingCount > 0 {
		report.Stats.Health = "degraded"
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
		Version:      plan.Version,
		Health:       "healthy",
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

func statsForManifest(record PlanRecord, manifest Manifest) PlanStats {
	stats := PlanStats{
		PlanID:       record.ID,
		Name:         unicodePreview(record.Name, taskPreviewRunes),
		Version:      record.Version,
		Health:       "healthy",
		Total:        len(manifest.Tasks),
		StatusCounts: map[Status]int{},
		NextSequence: manifest.NextSequence,
		UpdatedAt:    record.UpdatedAt.UTC(),
	}
	statuses := make(map[string]Status, len(manifest.Tasks))
	for _, task := range manifest.Tasks {
		statuses[task.ID] = task.Status
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
	for _, task := range manifest.Tasks {
		if task.Status != StatusNotStarted || task.ClaimedBy != "" {
			continue
		}
		ready := true
		for _, dependencyID := range task.DependsOn {
			if statuses[dependencyID] != StatusComplete {
				ready = false
				break
			}
		}
		if ready {
			stats.ReadyCount++
		}
	}
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

func (s *Store) auditOrphanTaskFiles(planID string, knownPaths map[string]string) []AuditFinding {
	findings := []AuditFinding{}
	for _, dir := range []string{s.planTasksDir(planID), s.planHistoryDir(planID)} {
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
				Code:     "orphan_task_record",
				Message:  "task record is not referenced by the manifest",
				Path:     auditRelativePath(s.planDir(planID), path),
			})
		}
	}
	return findings
}

func (s *Store) auditOrphanClaimFiles(planID string, activeClaims map[string]TaskSummary) []AuditFinding {
	entries, err := readDirFunc(s.planTasksDir(planID))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return []AuditFinding{{Severity: "error", Code: "read_dir_failed", Message: err.Error(), Path: tasksDirName}}
	}
	findings := []AuditFinding{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".claim") {
			continue
		}
		path := filepath.Join(s.planTasksDir(planID), entry.Name())
		if _, ok := activeClaims[filepath.Clean(path)]; ok {
			continue
		}
		findings = append(findings, AuditFinding{
			Severity: "warning",
			Code:     "orphan_claim",
			Message:  "claim file is not referenced by an active claimed task",
			Path:     filepath.ToSlash(filepath.Join(tasksDirName, entry.Name())),
		})
	}
	return findings
}

func auditRelativePath(planDir, path string) string {
	relative, err := filepath.Rel(planDir, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(relative)
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
