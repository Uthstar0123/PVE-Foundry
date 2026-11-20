package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"proxmox-orchestrator/internal/store"
)

func validJobID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "job_not_found", "Задача не найдена")
		return "", false
	}
	return id, true
}

func (m *Management) jobs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "Лимит должен быть от 1 до 100")
			return
		}
		limit = parsed
	}
	jobs, err := m.store.ListJobs(r.Context(), limit)
	if err != nil {
		log.Printf("list jobs: %v", err)
		writeError(w, http.StatusServiceUnavailable, "jobs_unavailable", "Не удалось получить задачи")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (m *Management) job(w http.ResponseWriter, r *http.Request) {
	id, ok := validJobID(w, r)
	if !ok {
		return
	}
	detail, err := m.store.Job(r.Context(), id)
	if err != nil {
		m.writeJobError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (m *Management) cancelJob(w http.ResponseWriter, r *http.Request) {
	id, ok := validJobID(w, r)
	if !ok {
		return
	}
	job, err := m.store.CancelJob(r.Context(), id)
	if err != nil {
		m.writeJobError(w, err)
		return
	}
	user := m.currentUser(r)
	if user != nil {
		_ = m.store.Audit(r.Context(), "job.cancel_requested_by_user", &user.ID, sourceIP(r), "job "+id+" cancellation requested; cancellation is not rollback")
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job, "rollback": false})
}

func (m *Management) jobLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := validJobID(w, r)
	if !ok {
		return
	}
	events, err := m.store.JobLogs(r.Context(), id)
	if err != nil {
		m.writeJobError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=job-%s.log", id))
	writer := bufio.NewWriter(w)
	for _, event := range events {
		_, _ = fmt.Fprintf(writer, "%s [%-5s] %s\n", event.CreatedAt.UTC().Format(time.RFC3339), event.Level, event.Message)
	}
	if err := writer.Flush(); err != nil {
		log.Printf("download job logs: %v", err)
	}
}

func (m *Management) audit(w http.ResponseWriter, r *http.Request) {
	events, err := m.store.ListAudit(r.Context(), 100)
	if err != nil {
		log.Printf("list audit: %v", err)
		writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "Не удалось получить аудит")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (m *Management) writeJobError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrResourceLocked) {
		writeError(w, http.StatusConflict, "resource_locked", "Ресурс уже используется другой задачей")
		return
	}
	if store.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "job_not_found", "Задача не найдена")
		return
	}
	log.Printf("job store: %v", err)
	writeError(w, http.StatusServiceUnavailable, "jobs_unavailable", "Хранилище задач недоступно")
}

func (m *Management) jobWebSocket(w http.ResponseWriter, r *http.Request) {
	id, ok := validJobID(w, r)
	if !ok {
		return
	}
	detail, err := m.store.Job(r.Context(), id)
	if err != nil {
		m.writeJobError(w, err)
		return
	}
	options := &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled}
	if !m.secure {
		options.OriginPatterns = []string{"localhost:7100", "127.0.0.1:7100"}
	}
	connection, err := websocket.Accept(w, r, options)
	if err != nil {
		log.Printf("accept job websocket: %v", err)
		return
	}
	defer connection.Close(websocket.StatusNormalClosure, "subscription complete")

	var previous []byte
	for {
		payload, err := json.Marshal(detail)
		if err == nil && !bytes.Equal(payload, previous) {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			err = connection.Write(ctx, websocket.MessageText, payload)
			cancel()
			if err != nil {
				return
			}
			previous = payload
		}
		if terminalJob(detail.Job.Status) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
		}
		detail, err = m.store.Job(r.Context(), id)
		if err != nil {
			_ = connection.Close(websocket.StatusInternalError, "job store unavailable")
			return
		}
	}
}

func terminalJob(status string) bool {
	return status == store.JobSucceeded || status == store.JobFailed || status == store.JobCancelled
}
