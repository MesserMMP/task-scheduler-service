package scheduler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", h.createTask)
	mux.HandleFunc("GET /tasks", h.listTasks)
	mux.HandleFunc("GET /tasks/{id}", h.getTask)
	mux.HandleFunc("POST /tasks/{id}/cancel", h.cancelTask)
	return mux
}

func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	var in CreateTaskInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if strings.TrimSpace(in.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if in.ScheduledAt.IsZero() {
		writeError(w, http.StatusBadRequest, "scheduled_at is required")
		return
	}

	in.ScheduledAt = in.ScheduledAt.UTC().Round(0)
	createdTask, err := h.service.CreateTask(r.Context(), in)
	if err != nil {
		h.logger.Error("create task", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, createdTask)
}

func (h *Handler) getTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := h.service.GetTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		h.logger.Error("get task", "task_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (h *Handler) listTasks(w http.ResponseWriter, r *http.Request) {
	var status *Status
	if rawStatus := strings.TrimSpace(r.URL.Query().Get("status")); rawStatus != "" {
		s := Status(rawStatus)
		if !isAllowedStatus(s) {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
		status = &s
	}

	items, err := h.service.ListTasks(r.Context(), status)
	if err != nil {
		h.logger.Error("list tasks", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items, "count": len(items), "timestamp": time.Now().UTC()})
}

func (h *Handler) cancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := h.service.CancelTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrCannotBeCanceled) {
			writeError(w, http.StatusConflict, "task cannot be canceled")
			return
		}
		h.logger.Error("cancel task", "task_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func isAllowedStatus(s Status) bool {
	switch s {
	case StatusPending, StatusRunning, StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
