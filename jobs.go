package orch8

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Job statuses reported by the engine.
const (
	JobScheduled    = "scheduled"
	JobRunning      = "running"
	JobCompleted    = "completed"
	JobFailed       = "failed"
	JobCancelled    = "cancelled"
	JobDeadLettered = "dead_lettered"
)

// IsTerminalJobStatus reports whether a job status will not change again.
func IsTerminalJobStatus(status string) bool {
	switch status {
	case JobCompleted, JobFailed, JobCancelled, JobDeadLettered:
		return true
	}
	return false
}

// JobRetry is a job's retry policy.
type JobRetry struct {
	MaxAttempts      int    `json:"max_attempts"`
	InitialBackoffMs int64  `json:"initial_backoff_ms"`
	MaxBackoffMs     *int64 `json:"max_backoff_ms,omitempty"`
}

// EnqueueJobRequest is the body of POST /jobs. Set at most one of DelayMs or
// RunAt. IdempotencyKey makes a repeated enqueue return the original job.
type EnqueueJobRequest struct {
	Handler        string         `json:"handler"`
	Payload        any            `json:"payload"`
	Queue          string         `json:"queue,omitempty"`
	Priority       *int           `json:"priority,omitempty"`
	Retry          *JobRetry      `json:"retry,omitempty"`
	DelayMs        *int64         `json:"delay_ms,omitempty"`
	RunAt          *time.Time     `json:"run_at,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// JobAttempt is one execution attempt of a job.
type JobAttempt struct {
	Attempt     int    `json:"attempt"`
	Status      string `json:"status,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       any    `json:"error,omitempty"`
}

// Job is a one-off background job. Attempts, Output, and Error are populated
// by GET /jobs/{id}.
type Job struct {
	ID         string         `json:"id"`
	InstanceID string         `json:"instance_id"`
	Handler    string         `json:"handler"`
	Status     string         `json:"status"`
	CreatedAt  string         `json:"created_at"`
	RunAt      string         `json:"run_at"`
	Queue      string         `json:"queue,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Attempts   []JobAttempt   `json:"attempts,omitempty"`
	Output     any            `json:"output,omitempty"`
	Error      any            `json:"error,omitempty"`
}

// JobListOptions filters GET /jobs.
type JobListOptions struct {
	Handler string
	Status  string
	Limit   int
	Cursor  string
}

// JobPage is one page of GET /jobs.
type JobPage struct {
	Items      []Job  `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// JobsService is the client for the engine's jobs API. The routes are not yet
// part of the generated contract (see APIRoutes); they target the jobs API
// shipping with the engine after 0.7.
type JobsService struct{ c *Client }

// Enqueue schedules a job.
func (s *JobsService) Enqueue(ctx context.Context, req EnqueueJobRequest) (*Job, error) {
	if req.Handler == "" {
		return nil, errors.New("orch8: job handler is required")
	}
	if req.DelayMs != nil && req.RunAt != nil {
		return nil, errors.New("orch8: set at most one of DelayMs and RunAt")
	}
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}
	var out Job
	if err := s.c.do(ctx, http.MethodPost, "/jobs", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Get fetches a job with its attempts, output, and error.
func (s *JobsService) Get(ctx context.Context, id string) (*Job, error) {
	var out Job
	if err := s.c.do(ctx, http.MethodGet, "/jobs/"+pathSegment(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Cancel cancels a job that has not reached a terminal status.
func (s *JobsService) Cancel(ctx context.Context, id string) error {
	return s.c.do(ctx, http.MethodDelete, "/jobs/"+pathSegment(id), nil, nil)
}

// ListPage fetches one page of jobs.
func (s *JobsService) ListPage(ctx context.Context, opts JobListOptions) (*JobPage, error) {
	values := url.Values{}
	if opts.Handler != "" {
		values.Set("handler", opts.Handler)
	}
	if opts.Status != "" {
		values.Set("status", opts.Status)
	}
	if opts.Limit > 0 {
		values.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Cursor != "" {
		values.Set("cursor", opts.Cursor)
	}
	path := "/jobs"
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	var out JobPage
	if err := s.c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns an iterator over every job matching opts, following cursors.
//
//	it := client.Jobs.List(ctx, orch8.JobListOptions{Status: orch8.JobFailed})
//	for it.Next() { job := it.Job() }
//	if err := it.Err(); err != nil { ... }
func (s *JobsService) List(ctx context.Context, opts JobListOptions) *JobIterator {
	return &JobIterator{s: s, ctx: ctx, opts: opts}
}

// JobIterator walks paginated job listings.
type JobIterator struct {
	s       *JobsService
	ctx     context.Context
	opts    JobListOptions
	buf     []Job
	cur     Job
	err     error
	started bool
	done    bool
}

// Next advances to the next job, fetching pages as needed.
func (it *JobIterator) Next() bool {
	for len(it.buf) == 0 {
		if it.err != nil || it.done {
			return false
		}
		if it.started && it.opts.Cursor == "" {
			it.done = true
			return false
		}
		it.started = true
		page, err := it.s.ListPage(it.ctx, it.opts)
		if err != nil {
			it.err = err
			return false
		}
		it.buf = page.Items
		if page.NextCursor == it.opts.Cursor && page.NextCursor != "" {
			it.err = errors.New("orch8: job listing cursor did not advance")
			return false
		}
		it.opts.Cursor = page.NextCursor
		if len(page.Items) == 0 && page.NextCursor == "" {
			it.done = true
			return false
		}
	}
	it.cur, it.buf = it.buf[0], it.buf[1:]
	return true
}

// Job returns the current job.
func (it *JobIterator) Job() Job { return it.cur }

// Err returns the first error encountered.
func (it *JobIterator) Err() error { return it.err }

// WaitOptions tunes WaitFor. Zero values use a 500ms interval and no timeout
// beyond ctx.
type WaitOptions struct {
	Interval time.Duration
	Timeout  time.Duration
}

// WaitFor polls a job until it reaches a terminal status (completed, failed,
// cancelled, dead_lettered) and returns it. It returns ctx's error (or
// context.DeadlineExceeded after Timeout) if the job is still running.
func (s *JobsService) WaitFor(ctx context.Context, id string, opts WaitOptions) (*Job, error) {
	interval := opts.Interval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	for {
		job, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if IsTerminalJobStatus(job.Status) {
			return job, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return job, ctx.Err()
		case <-timer.C:
		}
	}
}
