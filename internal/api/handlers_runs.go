package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"clicrontab/internal/core"
	"clicrontab/internal/store"

	"github.com/go-chi/chi/v5"
)

type runResponse struct {
	ID          string  `json:"id"`
	TaskID      string  `json:"task_id"`
	Status      string  `json:"status"`
	ScheduledAt string  `json:"scheduled_at"`
	StartedAt   *string `json:"started_at,omitempty"`
	EndedAt     *string `json:"ended_at,omitempty"`
	ExitCode    *int    `json:"exit_code,omitempty"`
	Error       *string `json:"error,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	run, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, store.ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "run not found")
		} else {
			s.logger.Error("get run", "run_id", runID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load run")
		}
		return
	}
	writeJSON(w, http.StatusOK, runToResponse(run))
}

func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	run, err := s.store.GetRun(r.Context(), runID)
	if err != nil {
		if errors.Is(err, store.ErrRunNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "run not found")
		} else {
			s.logger.Error("get run for log", "run_id", runID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load run")
		}
		return
	}

	tail := parseIntDefault(r.URL.Query().Get("tail"), 0)
	follow := strings.EqualFold(r.URL.Query().Get("follow"), "1") || strings.EqualFold(r.URL.Query().Get("follow"), "true")

	logPath := s.store.RunLogPath(runID)
	file, err := os.Open(logPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "not_found", "log not found")
		} else {
			s.logger.Error("open log", "run_id", runID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to read log")
		}
		return
	}
	defer file.Close()

	if !follow {
		data, err := readTailLines(file, tail)
		if err != nil {
			s.logger.Error("read log", "run_id", runID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to read log")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(data)
		return
	}

	if flusher, ok := w.(http.Flusher); ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")

		data, err := readTailLines(file, tail)
		if err != nil {
			s.logger.Error("read log", "run_id", runID, "err", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to read log")
			return
		}
		if len(data) > 0 {
			_, _ = w.Write(data)
			if len(data) > 0 && data[len(data)-1] != '\n' {
				_, _ = w.Write([]byte("\n"))
			}
			flusher.Flush()
		}

		offset, _ := file.Seek(0, io.SeekEnd)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				pos, err := file.Seek(0, io.SeekEnd)
				if err != nil {
					return
				}
				if pos > offset {
					buf := make([]byte, pos-offset)
					if _, err := file.ReadAt(buf, offset); err == nil {
						_, _ = w.Write(buf)
						flusher.Flush()
					}
					offset = pos
				}
				if !isRunFinished(run.Status) {
					if refreshed, err := s.store.GetRun(r.Context(), runID); err == nil {
						run = refreshed
					}
				}
				if isRunFinished(run.Status) && pos == offset {
					return
				}
			}
		}
	}

	writeError(w, http.StatusBadRequest, "unsupported", "streaming not supported")
}

func runToResponse(run *core.Run) runResponse {
	var started, ended *string
	if run.StartedAt != nil {
		formatted := run.StartedAt.UTC().Format(time.RFC3339)
		started = &formatted
	}
	if run.EndedAt != nil {
		formatted := run.EndedAt.UTC().Format(time.RFC3339)
		ended = &formatted
	}
	return runResponse{
		ID:          run.ID,
		TaskID:      run.TaskID,
		Status:      string(run.Status),
		ScheduledAt: run.ScheduledAt.UTC().Format(time.RFC3339),
		StartedAt:   started,
		EndedAt:     ended,
		ExitCode:    run.ExitCode,
		Error:       run.Error,
		CreatedAt:   run.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func readTailLines(file *os.File, tail int) ([]byte, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if tail <= 0 {
		return data, nil
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func isRunFinished(status core.RunStatus) bool {
	switch status {
	case core.RunStatusQueued, core.RunStatusRunning:
		return false
	default:
		return true
	}
}
