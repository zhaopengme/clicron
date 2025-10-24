package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clicrontab/internal/core"
	"clicrontab/internal/store"

	"github.com/go-chi/chi/v5"
)

type createTaskRequest struct {
	Name        *string `json:"name"`
	Command     string  `json:"command"`
	Cron        string  `json:"cron"`
	TimeoutSecs *int    `json:"timeout_s"`
	WorkingDir  *string `json:"working_dir"`
	Paused      bool    `json:"paused"`
}

type updateTaskRequest struct {
	Name        *string `json:"name"`
	Command     *string `json:"command"`
	Cron        *string `json:"cron"`
	TimeoutSecs *int    `json:"timeout_s"`
	WorkingDir  *string `json:"working_dir"`
	Paused      *bool   `json:"paused"`
}

type taskResponse struct {
	ID          string  `json:"id"`
	Name        *string `json:"name,omitempty"`
	Command     string  `json:"command"`
	Cron        string  `json:"cron"`
	TimeoutSecs *int    `json:"timeout_s,omitempty"`
	WorkingDir  *string `json:"working_dir,omitempty"`
	Status      string  `json:"status"`
	LastRunAt   *string `json:"last_run_at,omitempty"`
	NextRunAt   *string `json:"next_run_at,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON payload")
		return
	}

	req.Command = strings.TrimSpace(req.Command)
	req.Cron = strings.TrimSpace(req.Cron)
	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "command is required")
		return
	}
	if req.Cron == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "cron expression is required")
		return
	}
	if req.TimeoutSecs != nil && *req.TimeoutSecs < 0 {
		writeError(w, http.StatusBadRequest, "invalid_input", "timeout_s must be non-negative")
		return
	}

	schedule, err := core.ParseCron(req.Cron)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cron", err.Error())
		return
	}

	status := core.TaskStatusActive
	if req.Paused {
		status = core.TaskStatusPaused
	}

	var namePtr *string
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed != "" {
			namePtr = &trimmed
		}
	}

	var timeoutPtr *int
	if req.TimeoutSecs != nil && *req.TimeoutSecs > 0 {
		timeout := *req.TimeoutSecs
		timeoutPtr = &timeout
	}

	var workingDirPtr *string
	if req.WorkingDir != nil {
		trimmed := strings.TrimSpace(*req.WorkingDir)
		if trimmed != "" {
			workingDirPtr = &trimmed
		}
	}

	task := &core.Task{
		ID:             core.NewID(),
		Name:           namePtr,
		Command:        req.Command,
		Cron:           req.Cron,
		TimeoutSeconds: timeoutPtr,
		WorkingDir:     workingDirPtr,
		Status:         status,
	}

	if status == core.TaskStatusActive {
		next := core.NextOccurrences(schedule, time.Now().In(s.location), 1)[0].UTC()
		task.NextRunAt = &next
	}

	if err := s.store.InsertTask(r.Context(), task); err != nil {
		s.logger.Error("insert task", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to insert task")
		return
	}
	if task.Status == core.TaskStatusActive {
		if err := s.scheduler.AddOrUpdateTask(r.Context(), task); err != nil {
			s.logger.Error("schedule task", "task_id", task.ID, "err", err)
		}
	}

	writeJSON(w, http.StatusCreated, taskToResponse(task))
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	var statusFilter *core.TaskStatus
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		st := core.TaskStatus(status)
		switch st {
		case core.TaskStatusActive, core.TaskStatusPaused:
			statusFilter = &st
		default:
			writeError(w, http.StatusBadRequest, "invalid_input", "status must be active or paused")
			return
		}
	}
	tasks, err := s.store.ListTasks(r.Context(), statusFilter)
	if err != nil {
		s.logger.Error("list tasks", "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list tasks")
		return
	}
	res := make([]taskResponse, 0, len(tasks))
	for _, t := range tasks {
		res = append(res, taskToResponse(t))
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
		} else {
			s.logger.Error("get task", "task_id", taskID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load task")
		}
		return
	}
	writeJSON(w, http.StatusOK, taskToResponse(task))
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
		} else {
			s.logger.Error("get task for update", "task_id", taskID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load task")
		}
		return
	}

	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON payload")
		return
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			task.Name = nil
		} else {
			task.Name = &trimmed
		}
	}
	if req.Command != nil {
		cmd := strings.TrimSpace(*req.Command)
		if cmd == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "command cannot be empty")
			return
		}
		task.Command = cmd
	}

	cronChanged := false
	if req.Cron != nil {
		cronExpr := strings.TrimSpace(*req.Cron)
		if cronExpr == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "cron expression cannot be empty")
			return
		}
		if _, err := core.ParseCron(cronExpr); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cron", err.Error())
			return
		}
		task.Cron = cronExpr
		cronChanged = true
	}

	if req.TimeoutSecs != nil {
		if *req.TimeoutSecs < 0 {
			writeError(w, http.StatusBadRequest, "invalid_input", "timeout_s must be non-negative")
			return
		}
		if *req.TimeoutSecs == 0 {
			task.TimeoutSeconds = nil
		} else {
			timeout := *req.TimeoutSecs
			task.TimeoutSeconds = &timeout
		}
	}

	if req.WorkingDir != nil {
		trimmed := strings.TrimSpace(*req.WorkingDir)
		if trimmed == "" {
			task.WorkingDir = nil
		} else {
			task.WorkingDir = &trimmed
		}
	}

	statusChanged := false
	if req.Paused != nil {
		if *req.Paused && task.Status != core.TaskStatusPaused {
			task.Status = core.TaskStatusPaused
			statusChanged = true
		}
		if !*req.Paused && task.Status != core.TaskStatusActive {
			task.Status = core.TaskStatusActive
			statusChanged = true
		}
	}

	if task.Status == core.TaskStatusActive && (cronChanged || statusChanged) {
		parsed, err := core.ParseCron(task.Cron)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_cron", err.Error())
			return
		}
		next := core.NextOccurrences(parsed, time.Now().In(s.location), 1)[0].UTC()
		task.NextRunAt = &next
	}
	if task.Status == core.TaskStatusPaused {
		task.NextRunAt = nil
	}

	if err := s.store.UpdateTask(r.Context(), task); err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
			return
		}
		s.logger.Error("update task", "task_id", taskID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to update task")
		return
	}

	if err := s.scheduler.AddOrUpdateTask(r.Context(), task); err != nil {
		s.logger.Error("reschedule task", "task_id", task.ID, "err", err)
	}

	writeJSON(w, http.StatusOK, taskToResponse(task))
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if err := s.store.DeleteTask(r.Context(), taskID); err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
		} else {
			s.logger.Error("delete task", "task_id", taskID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete task")
		}
		return
	}
	s.scheduler.RemoveTask(taskID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
		} else {
			s.logger.Error("get task for run", "task_id", taskID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load task")
		}
		return
	}
	run, err := s.scheduler.RunTaskNow(r.Context(), task)
	if err != nil {
		if strings.Contains(err.Error(), "already running") {
			writeError(w, http.StatusConflict, "conflict", "task is already running")
			return
		}
		s.logger.Error("run task now", "task_id", taskID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to start task")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": run.ID})
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if _, err := s.store.GetTask(r.Context(), taskID); err != nil {
		if errors.Is(err, store.ErrTaskNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "task not found")
		} else {
			s.logger.Error("get task for runs list", "task_id", taskID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load task")
		}
		return
	}

	limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	runs, err := s.store.ListRuns(r.Context(), taskID, limit, offset)
	if err != nil {
		s.logger.Error("list runs", "task_id", taskID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list runs")
		return
	}

	resp := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		resp = append(resp, runToResponse(run))
	}
	writeJSON(w, http.StatusOK, resp)
}

func taskToResponse(task *core.Task) taskResponse {
	var last, next *string
	if task.LastRunAt != nil {
		formatted := task.LastRunAt.UTC().Format(time.RFC3339)
		last = &formatted
	}
	if task.NextRunAt != nil {
		formatted := task.NextRunAt.UTC().Format(time.RFC3339)
		next = &formatted
	}
	return taskResponse{
		ID:          task.ID,
		Name:        task.Name,
		Command:     task.Command,
		Cron:        task.Cron,
		TimeoutSecs: task.TimeoutSeconds,
		WorkingDir:  task.WorkingDir,
		Status:      string(task.Status),
		LastRunAt:   last,
		NextRunAt:   next,
		CreatedAt:   task.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   task.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func parseIntDefault(value string, def int) int {
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return parsed
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	payload := map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	}
	writeJSON(w, status, payload)
}
