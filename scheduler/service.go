package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type Service struct {
	repo           Repository
	httpClient     HTTPClient
	retryBaseDelay time.Duration
	logger         *slog.Logger
	wg             sync.WaitGroup
}

func NewService(repo Repository, httpClient HTTPClient, retryBaseDelay time.Duration, logger *slog.Logger) *Service {
	return &Service{
		repo:           repo,
		httpClient:     httpClient,
		retryBaseDelay: retryBaseDelay,
		logger:         logger,
	}
}

func (s *Service) CreateTask(ctx context.Context, in CreateTaskInput) (*Task, error) {
	in.Method = strings.ToUpper(strings.TrimSpace(in.Method))
	if in.Method == "" {
		in.Method = http.MethodGet
	}
	if in.MaxRetries < 0 {
		in.MaxRetries = 0
	}
	if in.Headers == nil {
		in.Headers = map[string]string{}
	}
	return s.repo.Create(ctx, in)
}

func (s *Service) GetTask(ctx context.Context, id string) (*Task, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) ListTasks(ctx context.Context, status *Status) ([]Task, error) {
	return s.repo.List(ctx, status)
}

func (s *Service) CancelTask(ctx context.Context, id string) error {
	ok, err := s.repo.CancelPending(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrCannotBeCanceled
	}
	return nil
}

func (s *Service) StartScheduler(ctx context.Context, interval time.Duration, batchSize int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runTick(ctx, batchSize)
		}
	}
}

func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) runTick(ctx context.Context, batchSize int) {
	tasks, err := s.repo.ClaimDue(ctx, batchSize)
	if err != nil {
		s.logger.Error("claim due tasks", "error", err)
		return
	}

	for _, t := range tasks {
		taskCopy := t
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.executeTask(ctx, taskCopy)
		}()
	}
}

func (s *Service) executeTask(ctx context.Context, t Task) {
	request, err := http.NewRequestWithContext(ctx, t.Method, t.URL, bytes.NewBufferString(t.Body))
	if err != nil {
		s.handleFailure(ctx, t, fmt.Sprintf("build request: %v", err), nil, nil, nil)
		return
	}
	for key, value := range t.Headers {
		request.Header.Set(key, value)
	}

	startedAt := time.Now()
	response, err := s.httpClient.Do(request)
	durationMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		s.handleFailure(ctx, t, fmt.Sprintf("http request: %v", err), nil, nil, nil)
		return
	}
	defer response.Body.Close()

	bodyBytes, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		s.handleFailure(ctx, t, fmt.Sprintf("read response body: %v", readErr), nil, nil, nil)
		return
	}

	body := string(bodyBytes)
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		if err := s.repo.MarkCompleted(ctx, t.ID, t.Attempt+1, response.StatusCode, body, durationMS); err != nil {
			s.logger.Error("mark completed", "task_id", t.ID, "error", err)
		}
		return
	}

	statusCode := response.StatusCode
	bodyCopy := body
	durationCopy := durationMS
	s.handleFailure(ctx, t, fmt.Sprintf("unexpected status code: %d, body: %s", response.StatusCode, body), &statusCode, &bodyCopy, &durationCopy)
}

func (s *Service) handleFailure(ctx context.Context, t Task, msg string, responseStatus *int, responseBody *string, durationMS *int64) {
	attempt := t.Attempt + 1
	if attempt <= t.MaxRetries {
		delay := s.retryBaseDelay * time.Duration(1<<(attempt-1))
		nextRetryAt := time.Now().Add(delay).UTC()
		if err := s.repo.MarkRetry(ctx, t.ID, attempt, msg, nextRetryAt, responseStatus, responseBody, durationMS); err != nil {
			s.logger.Error("mark retry", "task_id", t.ID, "error", err)
		}
		return
	}

	if err := s.repo.MarkFailed(ctx, t.ID, attempt, msg, responseStatus, responseBody, durationMS); err != nil {
		s.logger.Error("mark failed", "task_id", t.ID, "error", err)
	}
}
