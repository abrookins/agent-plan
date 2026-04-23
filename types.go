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

type TaskRecord struct {
	ID            string     `json:"id" yaml:"id"`
	Sequence      int        `json:"sequence" yaml:"sequence"`
	Slug          string     `json:"slug" yaml:"slug"`
	Title         string     `json:"title" yaml:"title"`
	Status        Status     `json:"status" yaml:"status"`
	Order         int        `json:"order" yaml:"order"`
	DependsOn     []string   `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	Assignee      string     `json:"assignee,omitempty" yaml:"assignee,omitempty"`
	BlockedReason string     `json:"blocked_reason,omitempty" yaml:"blocked_reason,omitempty"`
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
	Title              string     `json:"title"`
	Status             Status     `json:"status"`
	Order              int        `json:"order"`
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
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	NextSequence int           `json:"next_sequence"`
	Tasks        []TaskSummary `json:"tasks"`
}

type AddOptions struct {
	Description string
	DependsOn   []string
	Order       *int
}

type ListOptions struct {
	Status *Status
}

type ClaimInfo struct {
	By        string    `json:"by"`
	ClaimedAt time.Time `json:"claimed_at"`
	Host      string    `json:"host,omitempty"`
	PID       int       `json:"pid,omitempty"`
}

type Overview struct {
	Manifest Manifest       `json:"manifest"`
	Counts   map[string]int `json:"counts"`
	Next     *TaskSummary   `json:"next,omitempty"`
}
