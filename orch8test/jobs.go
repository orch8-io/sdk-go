package orch8test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	orch8 "github.com/orch8-io/sdk-go"
)

// Job returns a snapshot of a job.
func (e *Engine) Job(id string) (orch8.Job, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job, ok := e.jobs[id]
	if !ok {
		return orch8.Job{}, false
	}
	return job.snapshot(), true
}

// JobTaskID returns the worker task backing a job.
func (e *Engine) JobTaskID(jobID string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if job, ok := e.jobs[jobID]; ok {
		return job.taskID
	}
	return ""
}

func (j *jobRecord) snapshot() orch8.Job {
	out := j.Job
	out.Attempts = append([]orch8.JobAttempt(nil), j.Attempts...)
	return out
}

func (e *Engine) jobForLocked(rec *TaskRecord) *jobRecord {
	if rec.jobID == "" {
		return nil
	}
	return e.jobs[rec.jobID]
}

// syncJobLocked projects a task's state onto its job.
func (e *Engine) syncJobLocked(rec *TaskRecord) {
	job := e.jobForLocked(rec)
	if job == nil {
		return
	}
	now := e.now.Format(time.RFC3339Nano)
	last := func() *orch8.JobAttempt {
		if len(job.Attempts) == 0 {
			return nil
		}
		return &job.Attempts[len(job.Attempts)-1]
	}
	switch rec.State {
	case StatePending:
		if a := last(); a != nil && a.Status == orch8.JobRunning {
			a.Status, a.CompletedAt, a.Error = orch8.JobFailed, now, rec.FailMessage
		}
		job.Status = orch8.JobScheduled
	case StateClaimed:
		job.Status = orch8.JobRunning
		job.Attempts = append(job.Attempts, orch8.JobAttempt{Attempt: rec.Attempt + 1, Status: orch8.JobRunning, StartedAt: now})
	case StateCompleted:
		job.Status = orch8.JobCompleted
		job.Output = rec.Output
		if a := last(); a != nil {
			a.Status, a.CompletedAt = orch8.JobCompleted, now
		}
	case StateFailed:
		job.Status = orch8.JobFailed
		if rec.FailRetryable && job.maxAttempts > 1 {
			job.Status = orch8.JobDeadLettered
		}
		job.Error = map[string]any{"message": rec.FailMessage, "retryable": rec.FailRetryable}
		if a := last(); a != nil {
			a.Status, a.CompletedAt, a.Error = orch8.JobFailed, now, rec.FailMessage
		}
	case StateCancelled:
		job.Status = orch8.JobCancelled
	}
}

func (e *Engine) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Handler        string          `json:"handler"`
			Payload        any             `json:"payload"`
			Queue          string          `json:"queue"`
			Priority       *int            `json:"priority"`
			Retry          *orch8.JobRetry `json:"retry"`
			DelayMs        *int64          `json:"delay_ms"`
			RunAt          *time.Time      `json:"run_at"`
			IdempotencyKey string          `json:"idempotency_key"`
			Metadata       map[string]any  `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if req.Handler == "" {
			writeErr(w, http.StatusUnprocessableEntity, "handler is required")
			return
		}
		if req.DelayMs != nil && req.RunAt != nil {
			writeErr(w, http.StatusUnprocessableEntity, "delay_ms and run_at are mutually exclusive")
			return
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		if req.IdempotencyKey != "" {
			for _, id := range e.jobOrder {
				if job := e.jobs[id]; job.idemKey == req.IdempotencyKey {
					writeJSON(w, http.StatusOK, job.snapshot())
					return
				}
			}
		}
		runAt := e.now
		if req.DelayMs != nil {
			runAt = runAt.Add(time.Duration(*req.DelayMs) * time.Millisecond)
		}
		if req.RunAt != nil {
			runAt = req.RunAt.UTC()
		}
		inst := e.createInstanceLocked(map[string]any{"metadata": req.Metadata})
		inst.State = "running"
		opts := []TaskOption{WithInstance(inst.ID), WithNotBefore(runAt)}
		if req.Queue != "" {
			opts = append(opts, WithQueue(req.Queue))
		}
		rec := e.enqueueLocked(req.Handler, req.Payload, opts...)
		maxAttempts := 1
		if req.Retry != nil && req.Retry.MaxAttempts > 0 {
			maxAttempts = req.Retry.MaxAttempts
		}
		job := &jobRecord{
			Job: orch8.Job{
				ID: newID(), InstanceID: inst.ID, Handler: req.Handler, Status: orch8.JobScheduled,
				CreatedAt: e.now.Format(time.RFC3339Nano), RunAt: runAt.Format(time.RFC3339Nano),
				Queue: req.Queue, Metadata: req.Metadata,
			},
			taskID: rec.ID, maxAttempts: maxAttempts, idemKey: req.IdempotencyKey,
		}
		rec.jobID = job.ID
		e.jobs[job.ID] = job
		e.jobOrder = append(e.jobOrder, job.ID)
		writeJSON(w, http.StatusCreated, job.snapshot())
	case http.MethodGet:
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 {
			limit = 50
		}
		start, _ := strconv.Atoi(q.Get("cursor"))
		e.mu.Lock()
		page := orch8.JobPage{Items: []orch8.Job{}}
		i := start
		for ; i < len(e.jobOrder) && len(page.Items) < limit; i++ {
			job := e.jobs[e.jobOrder[i]]
			if (q.Get("handler") != "" && job.Handler != q.Get("handler")) || (q.Get("status") != "" && job.Status != q.Get("status")) {
				continue
			}
			page.Items = append(page.Items, job.snapshot())
		}
		if i < len(e.jobOrder) {
			page.NextCursor = strconv.Itoa(i)
			page.HasMore = true
		}
		e.mu.Unlock()
		writeJSON(w, http.StatusOK, page)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (e *Engine) handleJob(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/jobs/")
	e.mu.Lock()
	defer e.mu.Unlock()
	job, ok := e.jobs[id]
	if !ok {
		writeErr(w, http.StatusNotFound, "job "+id)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, job.snapshot())
	case http.MethodDelete:
		if orch8.IsTerminalJobStatus(job.Status) {
			writeErr(w, http.StatusConflict, "job is already "+job.Status)
			return
		}
		rec := e.tasks[job.taskID]
		rec.State = StateCancelled
		e.syncJobLocked(rec)
		e.notifyLocked()
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
