package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) EnsureSchema(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY,
    url TEXT NOT NULL,
    method TEXT NOT NULL,
    headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    body TEXT NOT NULL DEFAULT '',
    scheduled_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    max_retries INT NOT NULL DEFAULT 0,
    attempt INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ,
    last_error TEXT,
    response_status INT,
    response_body TEXT,
    duration_ms BIGINT,
    executed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_tasks_status_due ON tasks (status, scheduled_at, next_retry_at);
`)
	return err
}

func (r *PostgresRepository) Create(ctx context.Context, in CreateTaskInput) (*Task, error) {
	id := uuid.New().String()
	headersBytes, err := json.Marshal(in.Headers)
	if err != nil {
		return nil, err
	}

	row := r.pool.QueryRow(ctx, `
INSERT INTO tasks (id, url, method, headers, body, scheduled_at, status, max_retries)
VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8)
RETURNING id, url, method, headers, body, scheduled_at, status, max_retries, attempt,
          next_retry_at, last_error, response_status, response_body, duration_ms, executed_at,
          created_at, updated_at
`, id, in.URL, in.Method, string(headersBytes), in.Body, in.ScheduledAt, StatusPending, in.MaxRetries)

	return scanTask(row)
}

func (r *PostgresRepository) GetByID(ctx context.Context, id string) (*Task, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, url, method, headers, body, scheduled_at, status, max_retries, attempt,
       next_retry_at, last_error, response_status, response_body, duration_ms, executed_at,
       created_at, updated_at
FROM tasks
WHERE id = $1
`, id)
	return scanTask(row)
}

func (r *PostgresRepository) List(ctx context.Context, status *Status) ([]Task, error) {
	q := `
SELECT id, url, method, headers, body, scheduled_at, status, max_retries, attempt,
       next_retry_at, last_error, response_status, response_body, duration_ms, executed_at,
       created_at, updated_at
FROM tasks
`
	args := []any{}
	if status != nil {
		q += ` WHERE status = $1`
		args = append(args, *status)
	}
	q += ` ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *t)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return result, nil
}

func (r *PostgresRepository) CancelPending(ctx context.Context, id string) (bool, error) {
	cmd, err := r.pool.Exec(ctx, `
UPDATE tasks
SET status = $2, updated_at = NOW()
WHERE id = $1 AND status = $3
`, id, StatusCancelled, StatusPending)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() == 1, nil
}

func (r *PostgresRepository) ClaimDue(ctx context.Context, limit int) ([]Task, error) {
	rows, err := r.pool.Query(ctx, `
WITH due AS (
    SELECT id
    FROM tasks
    WHERE status = $1
      AND COALESCE(next_retry_at, scheduled_at) <= NOW()
    ORDER BY COALESCE(next_retry_at, scheduled_at), created_at
    FOR UPDATE SKIP LOCKED
    LIMIT $2
)
UPDATE tasks t
SET status = $3, updated_at = NOW()
FROM due
WHERE t.id = due.id
RETURNING t.id, t.url, t.method, t.headers, t.body, t.scheduled_at, t.status, t.max_retries, t.attempt,
          t.next_retry_at, t.last_error, t.response_status, t.response_body, t.duration_ms, t.executed_at,
          t.created_at, t.updated_at
`, StatusPending, limit, StatusRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *t)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return result, nil
}

func (r *PostgresRepository) MarkCompleted(ctx context.Context, id string, attempt int, responseStatus int, responseBody string, durationMS int64) error {
	_, err := r.pool.Exec(ctx, `
UPDATE tasks
SET status = $2,
    attempt = $3,
    response_status = $4,
    response_body = $5,
    duration_ms = $6,
    executed_at = NOW(),
    last_error = NULL,
    next_retry_at = NULL,
    updated_at = NOW()
WHERE id = $1
`, id, StatusCompleted, attempt, responseStatus, responseBody, durationMS)
	return err
}

func (r *PostgresRepository) MarkRetry(ctx context.Context, id string, attempt int, lastError string, nextRetryAt time.Time, responseStatus *int, responseBody *string, durationMS *int64) error {
	_, err := r.pool.Exec(ctx, `
UPDATE tasks
SET status = $2,
    attempt = $3,
    last_error = $4,
    next_retry_at = $5,
	response_status = $6,
	response_body = $7,
	duration_ms = $8,
    updated_at = NOW()
WHERE id = $1
`, id, StatusPending, attempt, lastError, nextRetryAt, responseStatus, responseBody, durationMS)
	return err
}

func (r *PostgresRepository) MarkFailed(ctx context.Context, id string, attempt int, lastError string, responseStatus *int, responseBody *string, durationMS *int64) error {
	_, err := r.pool.Exec(ctx, `
UPDATE tasks
SET status = $2,
    attempt = $3,
    last_error = $4,
	response_status = $5,
	response_body = $6,
	duration_ms = $7,
    next_retry_at = NULL,
    executed_at = NOW(),
    updated_at = NOW()
WHERE id = $1
`, id, StatusFailed, attempt, lastError, responseStatus, responseBody, durationMS)
	return err
}

func scanTask(row pgx.Row) (*Task, error) {
	var t Task
	var rawHeaders []byte
	err := row.Scan(
		&t.ID,
		&t.URL,
		&t.Method,
		&rawHeaders,
		&t.Body,
		&t.ScheduledAt,
		&t.Status,
		&t.MaxRetries,
		&t.Attempt,
		&t.NextRetryAt,
		&t.LastError,
		&t.ResponseStatus,
		&t.ResponseBody,
		&t.DurationMS,
		&t.ExecutedAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if len(rawHeaders) == 0 {
		t.Headers = map[string]string{}
	} else {
		if err := json.Unmarshal(rawHeaders, &t.Headers); err != nil {
			return nil, err
		}
	}
	if t.Headers == nil {
		t.Headers = map[string]string{}
	}

	return &t, nil
}
