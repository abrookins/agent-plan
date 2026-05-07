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
	SessionIDs  []string  `json:"session_ids,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int       `json:"version"`
}

type PlanSummary struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SpecPath      string    `json:"spec_path,omitempty"`
	SessionIDs    []string  `json:"session_ids,omitempty"`
	TaskCount     int       `json:"task_count"`
	CompleteCount int       `json:"complete_count"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type TaskRecord struct {
	ID                string     `json:"id" yaml:"id"`
	Sequence          int        `json:"sequence" yaml:"sequence"`
	Slug              string     `json:"slug" yaml:"slug"`
	Name              string     `json:"name" yaml:"name,omitempty"`
	Status            Status     `json:"status" yaml:"status"`
	Order             int        `json:"order" yaml:"order"`
	ParentID          string     `json:"parent_id,omitempty" yaml:"parent_id,omitempty"`
	AssignedSessionID string     `json:"assigned_session_id,omitempty" yaml:"assigned_session_id,omitempty"`
	DependsOn         []string   `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Assignee          string     `json:"assignee,omitempty" yaml:"assignee,omitempty"`
	BlockedReason     string     `json:"blocked_reason,omitempty" yaml:"blocked_reason,omitempty"`
	Output            string     `json:"output,omitempty" yaml:"output,omitempty"`
	CreatedAt         time.Time  `json:"created_at" yaml:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" yaml:"updated_at"`
	ClaimedBy         string     `json:"claimed_by,omitempty" yaml:"claimed_by,omitempty"`
	ClaimedAt         *time.Time `json:"claimed_at,omitempty" yaml:"claimed_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
	ArchivedAt        *time.Time `json:"archived_at,omitempty" yaml:"archived_at,omitempty"`
	Description       string     `json:"description" yaml:"-"`
	Path              string     `json:"path,omitempty" yaml:"-"`
	ClaimPath         string     `json:"claim_path,omitempty" yaml:"-"`
}

type TaskSummary struct {
	ID                 string     `json:"id"`
	Sequence           int        `json:"sequence"`
	Name               string     `json:"name"`
	Status             Status     `json:"status"`
	Order              int        `json:"order"`
	ParentID           string     `json:"parent_id,omitempty"`
	AssignedSessionID  string     `json:"assigned_session_id,omitempty"`
	DependsOn          []string   `json:"depends_on,omitempty"`
	Assignee           string     `json:"assignee,omitempty"`
	BlockedReason      string     `json:"blocked_reason,omitempty"`
	Output             string     `json:"output,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	ClaimedBy          string     `json:"claimed_by,omitempty"`
	ClaimedAt          *time.Time `json:"claimed_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	ArchivedAt         *time.Time `json:"archived_at,omitempty"`
	Path               string     `json:"path"`
	ClaimPath          string     `json:"claim_path,omitempty"`
	DescriptionPreview string     `json:"description_preview,omitempty"`
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
	SessionIDs  []string     `json:"session_ids,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	Version     int          `json:"version"`
}

type SessionBinding struct {
	SessionID       string     `json:"session_id"`
	PlanID          string     `json:"plan_id"`
	PlanName        string     `json:"plan_name"`
	CurrentTaskID   string     `json:"current_task_id,omitempty"`
	CurrentTaskName string     `json:"current_task_name,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ClaimedAt       *time.Time `json:"claimed_at,omitempty"`
}

type AddOptions struct {
	Description string
	ParentID    string
	DependsOn   []string
	Order       *int
}

type PlanUpdateOptions struct {
	Name          *string
	Description   *string
	SpecPath      *string
	SessionIDs    []string
	ClearSessions bool
}

type TaskUpdateOptions struct {
	Status               *Status
	Name                 *string
	Description          *string
	AssignedSessionID    *string
	ClearAssignedSession bool
	DependsOn            []string
	ClearDependencies    bool
	Order                *int
}

type ClaimInfo struct {
	By        string    `json:"by"`
	ClaimedAt time.Time `json:"claimed_at"`
	Host      string    `json:"host,omitempty"`
	PID       int       `json:"pid,omitempty"`
}
