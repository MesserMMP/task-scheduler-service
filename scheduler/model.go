package scheduler

import "time"

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Task struct {
	ID             string            `json:"id"`
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	ScheduledAt    time.Time         `json:"scheduled_at"`
	Status         Status            `json:"status"`
	MaxRetries     int               `json:"max_retries"`
	Attempt        int               `json:"attempt"`
	NextRetryAt    *time.Time        `json:"next_retry_at,omitempty"`
	LastError      *string           `json:"last_error,omitempty"`
	ResponseStatus *int              `json:"response_status,omitempty"`
	ResponseBody   *string           `json:"response_body,omitempty"`
	DurationMS     *int64            `json:"duration_ms,omitempty"`
	ExecutedAt     *time.Time        `json:"executed_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

type CreateTaskInput struct {
	URL         string            `json:"url"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	Body        string            `json:"body"`
	ScheduledAt time.Time         `json:"scheduled_at"`
	MaxRetries  int               `json:"max_retries"`
}
