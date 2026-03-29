package scheduler

import (
	"context"
	"time"
)

type Repository interface {
	EnsureSchema(ctx context.Context) error
	Create(ctx context.Context, in CreateTaskInput) (*Task, error)
	GetByID(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context, status *Status) ([]Task, error)
	CancelPending(ctx context.Context, id string) (bool, error)
	ClaimDue(ctx context.Context, limit int) ([]Task, error)
	MarkCompleted(ctx context.Context, id string, attempt int, responseStatus int, responseBody string, durationMS int64) error
	MarkRetry(ctx context.Context, id string, attempt int, lastError string, nextRetryAt time.Time, responseStatus *int, responseBody *string, durationMS *int64) error
	MarkFailed(ctx context.Context, id string, attempt int, lastError string, responseStatus *int, responseBody *string, durationMS *int64) error
}
