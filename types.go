package agentplan

import "time"

type Status string

const (
	StatusNotStarted Status = "not_started"
	StatusInProgress Status = "in_progress"
	StatusBlocked    Status = "blocked"
	StatusComplete   Status = "complete"
	StatusCancelled  Status = "cancelled"
)

type Workspace struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type PlanRecord struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SpecPath    string    `json:"spec_path,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int       `json:"version"`
}

type PlanSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SpecPath      string    `json:"spec_path,omitempty"`
	TaskCount     int       `json:"task_count"`
	CompleteCount int       `json:"complete_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type TaskRecord struct {
	ID            string     `json:"id" yaml:"id"`
	Sequence      int        `json:"sequence" yaml:"sequence"`
	Slug          string     `json:"slug" yaml:"slug"`
	ImportKey     string     `json:"import_key,omitempty" yaml:"import_key,omitempty"`
	Name          string     `json:"name" yaml:"name,omitempty"`
	Status        Status     `json:"status" yaml:"status"`
	Order         int        `json:"order" yaml:"order"`
	ParentID      string     `json:"parent_id,omitempty" yaml:"parent_id,omitempty"`
	DependsOn     []string   `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Assignee      string     `json:"assignee,omitempty" yaml:"assignee,omitempty"`
	BlockedReason string     `json:"blocked_reason,omitempty" yaml:"blocked_reason,omitempty"`
	Result        string     `json:"result,omitempty" yaml:"result,omitempty"`
	Output        string     `json:"output,omitempty" yaml:"output,omitempty"`
	CreatedAt     time.Time  `json:"created_at" yaml:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" yaml:"updated_at"`
	ClaimedBy     string     `json:"claimed_by,omitempty" yaml:"claimed_by,omitempty"`
	ClaimedAt     *time.Time `json:"claimed_at,omitempty" yaml:"claimed_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
	ArchivedAt    *time.Time `json:"archived_at,omitempty" yaml:"archived_at,omitempty"`
	Description   string     `json:"description" yaml:"-"`
	Path          string     `json:"path,omitempty" yaml:"-"`
	ClaimPath     string     `json:"claim_path,omitempty" yaml:"-"`
}

type TaskSummary struct {
	ID                 string     `json:"id"`
	Sequence           int        `json:"sequence"`
	ImportKey          string     `json:"import_key,omitempty"`
	Name               string     `json:"name"`
	Status             Status     `json:"status"`
	Order              int        `json:"order"`
	ParentID           string     `json:"parent_id,omitempty"`
	DependsOn          []string   `json:"depends_on,omitempty"`
	Assignee           string     `json:"assignee,omitempty"`
	BlockedReason      string     `json:"blocked_reason,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ClaimedBy          string     `json:"claimed_by,omitempty"`
	ClaimedAt          *time.Time `json:"claimed_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	Path               string     `json:"path"`
	ClaimPath          string     `json:"claim_path,omitempty"`
	DescriptionPreview string     `json:"description_preview,omitempty"`
	RecordHash         string     `json:"record_hash,omitempty"`
}

type Manifest struct {
	Version      int           `json:"version"`
	PlanID       string        `json:"plan_id"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	NextSequence int           `json:"next_sequence"`
	Tasks        []TaskSummary `json:"tasks"`
}

type Plan struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	SpecPath    string       `json:"spec_path,omitempty"`
	Tasks       []TaskRecord `json:"tasks"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Version     int          `json:"version"`
}

type AddOptions struct {
	Description string
	ParentID    string
	DependsOn   []string
	Order       *int
}

type PlanUpdateOptions struct {
	Name        *string
	Description *string
	SpecPath    *string
}

type TaskUpdateOptions struct {
	Status            *Status
	Name              *string
	Description       *string
	Result            *string
	ParentID          *string
	ClaimedBy         *string
	ClearClaimedBy    bool
	DependsOn         []string
	ClearDependencies bool
	Order             *int
}

type ClaimInfo struct {
	By        string    `json:"by"`
	ClaimedAt time.Time `json:"claimed_at"`
	Host      string    `json:"host,omitempty"`
	PID       int       `json:"pid,omitempty"`
}

type BulkImportSpec struct {
	Tasks  []BulkImportTaskSpec `json:"tasks"`
	Chains [][]string           `json:"chains,omitempty"`
}

type BulkImportTaskSpec struct {
	Key         string   `json:"key,omitempty"`
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name"`
	Description *string  `json:"description,omitempty"`
	Parent      *string  `json:"parent,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Status      string   `json:"status,omitempty"`
	ClaimedBy   *string  `json:"claimed_by,omitempty"`
	Order       *int     `json:"order,omitempty"`
}

type BulkImportOptions struct {
	DryRun bool
}

type BulkImportReport struct {
	PlanID    string             `json:"plan_id"`
	DryRun    bool               `json:"dry_run"`
	Total     int                `json:"total"`
	Created   int                `json:"created"`
	Updated   int                `json:"updated"`
	Unchanged int                `json:"unchanged"`
	Changes   []BulkImportChange `json:"changes"`
	Errors    []string           `json:"errors,omitempty"`
}

type BulkImportChange struct {
	Action string `json:"action"`
	Key    string `json:"key,omitempty"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

type PlanStats struct {
	PlanID          string         `json:"plan_id"`
	Name            string         `json:"name"`
	Version         int            `json:"version"`
	Health          string         `json:"health"`
	FindingCount    int            `json:"finding_count"`
	Total           int            `json:"total"`
	StatusCounts    map[Status]int `json:"status_counts"`
	RootCount       int            `json:"root_count"`
	ParentedCount   int            `json:"parented_count"`
	DependencyCount int            `json:"dependency_count"`
	ReadyCount      int            `json:"ready_count"`
	ClaimedCount    int            `json:"claimed_count"`
	HistoryCount    int            `json:"history_count"`
	NextSequence    int            `json:"next_sequence"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type AuditReport struct {
	PlanID   string         `json:"plan_id"`
	Stats    PlanStats      `json:"stats"`
	Findings []AuditFinding `json:"findings"`
}

type AuditFinding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	TaskID   string `json:"task_id,omitempty"`
	Path     string `json:"path,omitempty"`
}

type LockStatus struct {
	PlanID       string  `json:"plan_id"`
	Path         string  `json:"path"`
	Exists       bool    `json:"exists"`
	PID          int     `json:"pid,omitempty"`
	Host         string  `json:"host,omitempty"`
	CreatedAt    string  `json:"created_at,omitempty"`
	AgeSeconds   float64 `json:"age_seconds,omitempty"`
	OwnerAlive   *bool   `json:"owner_alive,omitempty"`
	CurrentHost  string  `json:"current_host,omitempty"`
	OwnerIsLocal bool    `json:"owner_is_local"`
}

type LockClearResult struct {
	Cleared bool       `json:"cleared"`
	Status  LockStatus `json:"status"`
}
