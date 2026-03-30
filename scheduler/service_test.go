package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

type mockRepo struct {
	markCompletedCalled bool
	markRetryCalled     bool
	markFailedCalled    bool

	completedAttempt int
	completedStatus  int
	completedBody    string
	completedMS      int64

	retryAttempt int
	retryError   string
	retryAt      time.Time

	failedAttempt int
	failedError   string
}

func (m *mockRepo) EnsureSchema(ctx context.Context) error { return nil }
func (m *mockRepo) Create(ctx context.Context, in CreateTaskInput) (*Task, error) {
	return nil, nil
}
func (m *mockRepo) GetByID(ctx context.Context, id string) (*Task, error)    { return nil, nil }
func (m *mockRepo) List(ctx context.Context, status *Status) ([]Task, error) { return nil, nil }
func (m *mockRepo) CancelPending(ctx context.Context, id string) (bool, error) {
	return false, nil
}
func (m *mockRepo) ClaimDue(ctx context.Context, limit int) ([]Task, error) { return nil, nil }

func (m *mockRepo) MarkCompleted(ctx context.Context, id string, attempt int, responseStatus int, responseBody string, durationMS int64) error {
	m.markCompletedCalled = true
	m.completedAttempt = attempt
	m.completedStatus = responseStatus
	m.completedBody = responseBody
	m.completedMS = durationMS
	return nil
}

func (m *mockRepo) MarkRetry(ctx context.Context, id string, attempt int, lastError string, nextRetryAt time.Time, responseStatus *int, responseBody *string, durationMS *int64) error {
	m.markRetryCalled = true
	m.retryAttempt = attempt
	m.retryError = lastError
	m.retryAt = nextRetryAt
	return nil
}

func (m *mockRepo) MarkFailed(ctx context.Context, id string, attempt int, lastError string, responseStatus *int, responseBody *string, durationMS *int64) error {
	m.markFailedCalled = true
	m.failedAttempt = attempt
	m.failedError = lastError
	return nil
}

type mockHTTPClient struct {
	resp *http.Response
	err  error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.resp, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestExecuteTaskSuccess(t *testing.T) {
	repo := &mockRepo{}
	client := &mockHTTPClient{resp: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}}
	svc := NewService(repo, client, time.Second, testLogger())

	svc.executeTask(context.Background(), Task{ID: "1", URL: "http://example.com", Method: http.MethodPost, Body: "payload", Attempt: 0, MaxRetries: 2})

	if !repo.markCompletedCalled {
		t.Fatalf("expected task to be marked completed")
	}
	if repo.markRetryCalled || repo.markFailedCalled {
		t.Fatalf("expected no retry/failed transitions")
	}
	if repo.completedAttempt != 1 {
		t.Fatalf("unexpected attempt value: got %d want 1", repo.completedAttempt)
	}
	if repo.completedStatus != http.StatusOK {
		t.Fatalf("unexpected response status: got %d", repo.completedStatus)
	}
	if repo.completedBody != "ok" {
		t.Fatalf("unexpected response body: got %q", repo.completedBody)
	}
}

func TestExecuteTaskRetryOnNon2xx(t *testing.T) {
	repo := &mockRepo{}
	client := &mockHTTPClient{resp: &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("bad"))}}
	svc := NewService(repo, client, 2*time.Second, testLogger())

	started := time.Now()
	svc.executeTask(context.Background(), Task{ID: "1", URL: "http://example.com", Method: http.MethodGet, Attempt: 0, MaxRetries: 2})

	if !repo.markRetryCalled {
		t.Fatalf("expected task to be marked for retry")
	}
	if repo.markCompletedCalled || repo.markFailedCalled {
		t.Fatalf("expected no completed/failed transitions")
	}
	if repo.retryAttempt != 1 {
		t.Fatalf("unexpected retry attempt: got %d want 1", repo.retryAttempt)
	}
	if repo.retryAt.Before(started.Add(2 * time.Second)) {
		t.Fatalf("retry backoff is too short")
	}
}

func TestExecuteTaskFailWhenRetriesExhausted(t *testing.T) {
	repo := &mockRepo{}
	client := &mockHTTPClient{err: errors.New("network down")}
	svc := NewService(repo, client, time.Second, testLogger())

	svc.executeTask(context.Background(), Task{ID: "1", URL: "http://example.com", Method: http.MethodGet, Attempt: 1, MaxRetries: 1})

	if !repo.markFailedCalled {
		t.Fatalf("expected task to be marked failed")
	}
	if repo.markCompletedCalled || repo.markRetryCalled {
		t.Fatalf("expected no completed/retry transitions")
	}
	if repo.failedAttempt != 2 {
		t.Fatalf("unexpected failed attempt: got %d want 2", repo.failedAttempt)
	}
}
