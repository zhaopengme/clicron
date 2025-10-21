package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"clicrontab/internal/core"
)

type cronPreviewRequest struct {
	Expr  string `json:"expr"`
	Now   string `json:"now,omitempty"`
	Count int    `json:"count,omitempty"`
}

type cronPreviewResponse struct {
	Valid     bool     `json:"valid"`
	NextTimes []string `json:"next_times,omitempty"`
	Message   string   `json:"message,omitempty"`
}

func (s *Server) handleCronPreview(w http.ResponseWriter, r *http.Request) {
	var req cronPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, cronPreviewResponse{Valid: false, Message: "invalid JSON payload"})
		return
	}
	expr := strings.TrimSpace(req.Expr)
	if expr == "" {
		writeJSON(w, http.StatusBadRequest, cronPreviewResponse{Valid: false, Message: "cron expression is required"})
		return
	}
	schedule, err := core.ParseCron(expr)
	if err != nil {
		writeJSON(w, http.StatusOK, cronPreviewResponse{Valid: false, Message: err.Error()})
		return
	}

	count := req.Count
	if count <= 0 || count > 10 {
		count = 5
	}

	base := time.Now().In(s.location)
	if req.Now != "" {
		if parsed, err := time.Parse(time.RFC3339, req.Now); err == nil {
			base = parsed.In(s.location)
		}
	}

	times := core.NextOccurrences(schedule, base, count)
	formatted := make([]string, 0, len(times))
	for _, t := range times {
		formatted = append(formatted, t.UTC().Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, cronPreviewResponse{Valid: true, NextTimes: formatted})
}
