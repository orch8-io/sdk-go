// Package orch8test provides an in-process fake Orch8 engine for tests.
//
// Engine is an httptest server that speaks the worker lease protocol (poll,
// queue poll, heartbeat, complete, fail with claim_epoch checks), a minimal
// instance store, and the jobs API. It does not interpret sequences: tests
// enqueue worker tasks directly (EnqueueTask) or through jobs, then run a real
// orch8.Worker or push handler against it and assert on the recorded results.
//
//	eng := orch8test.NewEngine(t)
//	task := eng.EnqueueTask("send-email", map[string]any{"to": "a@b.c"})
//	worker := orch8.NewWorker(orch8.WorkerConfig{Client: eng.Client(), WorkerID: "w", Handlers: handlers})
//	go worker.Start(ctx)
//	rec := eng.WaitForTask(t, task.ID, 5*time.Second)
//
// The engine has its own clock (Now/Advance) so delayed jobs and run_at can be
// tested without sleeping.
package orch8test

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	orch8 "github.com/orch8-io/sdk-go"
)

// Task states used by the fake engine (match the engine's worker task states).
const (
	StatePending   = "pending"
	StateClaimed   = "claimed"
	StateCompleted = "completed"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
)

// Options tunes the fake engine's advertised lease hints.
type Options struct {
	// LeaseSecs advertised on poll (default 30).
	LeaseSecs uint64
	// HeartbeatIntervalSecs advertised on poll (default 10).
	HeartbeatIntervalSecs uint64
	// EmptyPollAfterMs is advertised when a poll returns nothing. The real
	// engine uses 1000; the default here is 0 so tests stay fast.
	EmptyPollAfterMs uint64
	// Start is the initial fake clock time (default time.Now()).
	Start time.Time
}

// TaskRecord is the fake engine's view of a worker task.
type TaskRecord struct {
	orch8.WorkerTask
	// FailMessage and FailRetryable record the last fail request.
	FailMessage   string
	FailRetryable bool
	// Heartbeats counts accepted heartbeats; StaleRejections counts 409s.
	Heartbeats      int
	StaleRejections int
	notBefore       time.Time
	jobID           string
}

// Terminal reports whether the task will not be claimed again.
func (r TaskRecord) Terminal() bool {
	return r.State == StateCompleted || r.State == StateFailed || r.State == StateCancelled
}

type jobRecord struct {
	orch8.Job
	taskID      string
	maxAttempts int
	idemKey     string
}

// Engine is an in-memory fake Orch8 engine served over HTTP.
type Engine struct {
	Server *httptest.Server
	opts   Options

	mu        sync.Mutex
	now       time.Time
	tasks     map[string]*TaskRecord
	order     []string
	instances map[string]*orch8.TaskInstance
	jobs      map[string]*jobRecord
	jobOrder  []string
	changed   chan struct{}
}

// NewEngine starts a fake engine and closes it when the test ends.
func NewEngine(t testing.TB, opts ...Options) *Engine {
	t.Helper()
	e := NewUnstartedEngine(opts...)
	e.Server = httptest.NewServer(e.Handler())
	t.Cleanup(e.Server.Close)
	return e
}

// NewUnstartedEngine builds an engine without a server; mount Handler()
// yourself (e.g. behind httptest.NewTLSServer).
func NewUnstartedEngine(opts ...Options) *Engine {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.LeaseSecs == 0 {
		o.LeaseSecs = 30
	}
	if o.HeartbeatIntervalSecs == 0 {
		o.HeartbeatIntervalSecs = 10
	}
	start := o.Start
	if start.IsZero() {
		start = time.Now()
	}
	return &Engine{
		opts:      o,
		now:       start.UTC(),
		tasks:     map[string]*TaskRecord{},
		instances: map[string]*orch8.TaskInstance{},
		jobs:      map[string]*jobRecord{},
		changed:   make(chan struct{}),
	}
}

// URL is the base URL of the fake engine.
func (e *Engine) URL() string { return e.Server.URL }

// Client returns an SDK client pointed at the fake engine. cfg fields other
// than BaseURL are honored.
func (e *Engine) Client(cfg ...orch8.ClientConfig) *orch8.Client {
	var c orch8.ClientConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	c.BaseURL = e.Server.URL
	if c.RetryMaxAttempts == 0 {
		c.RetryMaxAttempts = 1
	}
	return orch8.NewClient(c)
}

// Now returns the fake engine clock.
func (e *Engine) Now() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

// Advance moves the fake clock forward, releasing delayed tasks and jobs.
func (e *Engine) Advance(d time.Duration) {
	e.mu.Lock()
	e.now = e.now.Add(d)
	e.notifyLocked()
	e.mu.Unlock()
}

func (e *Engine) notifyLocked() {
	close(e.changed)
	e.changed = make(chan struct{})
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// TaskOption customizes EnqueueTask.
type TaskOption func(*TaskRecord)

// WithQueue routes the task to a named queue.
func WithQueue(queue string) TaskOption { return func(r *TaskRecord) { r.QueueName = queue } }

// WithInstance sets the owning instance id.
func WithInstance(id string) TaskOption { return func(r *TaskRecord) { r.InstanceID = id } }

// WithBlock sets the block id.
func WithBlock(id string) TaskOption { return func(r *TaskRecord) { r.BlockID = id } }

// WithTimeout sets timeout_ms.
func WithTimeout(d time.Duration) TaskOption {
	return func(r *TaskRecord) { ms := int(d.Milliseconds()); r.TimeoutMs = &ms }
}

// WithContext sets the task context.
func WithContext(ctx any) TaskOption { return func(r *TaskRecord) { r.Context = ctx } }

// WithNotBefore delays claimability until the fake clock reaches t.
func WithNotBefore(t time.Time) TaskOption { return func(r *TaskRecord) { r.notBefore = t } }

// EnqueueTask adds a pending worker task and returns it.
func (e *Engine) EnqueueTask(handler string, params any, opts ...TaskOption) orch8.WorkerTask {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec := e.enqueueLocked(handler, params, opts...)
	return rec.WorkerTask
}

func (e *Engine) enqueueLocked(handler string, params any, opts ...TaskOption) *TaskRecord {
	if params == nil {
		params = map[string]any{}
	}
	rec := &TaskRecord{WorkerTask: orch8.WorkerTask{
		ID:          newID(),
		InstanceID:  newID(),
		BlockID:     handler,
		HandlerName: handler,
		Params:      params,
		Context:     map[string]any{},
		State:       StatePending,
		CreatedAt:   e.now.Format(time.RFC3339Nano),
	}}
	for _, opt := range opts {
		opt(rec)
	}
	e.tasks[rec.ID] = rec
	e.order = append(e.order, rec.ID)
	e.notifyLocked()
	return rec
}

// Task returns a snapshot of a task.
func (e *Engine) Task(id string) (TaskRecord, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.tasks[id]
	if !ok {
		return TaskRecord{}, false
	}
	return *rec, true
}

// Tasks returns snapshots of all tasks in enqueue order.
func (e *Engine) Tasks() []TaskRecord {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]TaskRecord, 0, len(e.order))
	for _, id := range e.order {
		out = append(out, *e.tasks[id])
	}
	return out
}

// WaitForTask blocks until the task is terminal and returns it, failing the
// test on timeout.
func (e *Engine) WaitForTask(t testing.TB, id string, timeout time.Duration) TaskRecord {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		e.mu.Lock()
		rec, ok := e.tasks[id]
		var snap TaskRecord
		if ok {
			snap = *rec
		}
		ch := e.changed
		e.mu.Unlock()
		if !ok {
			t.Fatalf("orch8test: unknown task %s", id)
		}
		if snap.Terminal() {
			return snap
		}
		select {
		case <-ch:
		case <-deadline.C:
			t.Fatalf("orch8test: task %s not terminal after %s (state %s)", id, timeout, snap.State)
			return snap
		}
	}
}

// ExpireLease simulates lease expiry and reclaim: the task returns to pending
// with a bumped epoch, so the old holder's acknowledgements get 409.
func (e *Engine) ExpireLease(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if rec, ok := e.tasks[id]; ok && rec.State == StateClaimed {
		rec.State = StatePending
		rec.WorkerID = ""
		rec.ClaimEpoch++
		e.notifyLocked()
	}
}

// PushEnvelope builds the body the engine would push for a task.
func (e *Engine) PushEnvelope(id string) []byte {
	e.mu.Lock()
	rec := e.tasks[id]
	e.mu.Unlock()
	if rec == nil {
		return nil
	}
	body, _ := json.Marshal(map[string]any{
		"task_id":      rec.ID,
		"instance_id":  rec.InstanceID,
		"block_id":     rec.BlockID,
		"handler_name": rec.HandlerName,
		"queue_name":   rec.QueueName,
		"params":       rec.Params,
		"context":      rec.Context,
		"attempt":      rec.Attempt,
		"timeout_ms":   rec.TimeoutMs,
	})
	return body
}

// SignedPushHeaders returns the headers the engine sends with a push signed
// by secret at the current wall-clock time.
func SignedPushHeaders(secret string, body []byte) http.Header {
	ts := time.Now().Unix()
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set(orch8.PushTimestampHeader, strconv.FormatInt(ts, 10))
	h.Set(orch8.PushSignatureHeader, orch8.SignPush(secret, ts, body))
	return h
}

// Handler returns the fake engine's HTTP handler.
func (e *Engine) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/workers/tasks/poll", e.handlePoll(false))
	mux.HandleFunc("/workers/tasks/poll/queue", e.handlePoll(true))
	mux.HandleFunc("/workers/tasks/", e.handleTaskAction)
	mux.HandleFunc("/instances", e.handleInstances)
	mux.HandleFunc("/instances/", e.handleInstance)
	mux.HandleFunc("/jobs", e.handleJobs)
	mux.HandleFunc("/jobs/", e.handleJob)
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func (e *Engine) handlePoll(byQueue bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var req struct {
			HandlerName string `json:"handler_name"`
			WorkerID    string `json:"worker_id"`
			QueueName   string `json:"queue_name"`
			Limit       int    `json:"limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.HandlerName == "" || req.WorkerID == "" {
			writeErr(w, http.StatusUnprocessableEntity, "handler_name and worker_id are required")
			return
		}
		if byQueue && req.QueueName == "" {
			writeErr(w, http.StatusUnprocessableEntity, "queue_name is required")
			return
		}
		if req.Limit <= 0 {
			req.Limit = 1
		}
		e.mu.Lock()
		claimed := []orch8.WorkerTask{}
		for _, id := range e.order {
			if len(claimed) >= req.Limit {
				break
			}
			rec := e.tasks[id]
			if rec.State != StatePending || rec.HandlerName != req.HandlerName || rec.notBefore.After(e.now) {
				continue
			}
			if byQueue && rec.QueueName != req.QueueName {
				continue
			}
			rec.State = StateClaimed
			rec.WorkerID = req.WorkerID
			rec.ClaimEpoch++
			rec.ClaimedAt = e.now.Format(time.RFC3339Nano)
			e.syncJobLocked(rec)
			claimed = append(claimed, rec.WorkerTask)
		}
		if len(claimed) > 0 {
			e.notifyLocked()
		}
		e.mu.Unlock()
		pollAfter := uint64(0)
		if len(claimed) == 0 {
			pollAfter = e.opts.EmptyPollAfterMs
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tasks":                   claimed,
			"lease_secs":              e.opts.LeaseSecs,
			"heartbeat_interval_secs": e.opts.HeartbeatIntervalSecs,
			"poll_after_ms":           pollAfter,
		})
	}
}

func (e *Engine) handleTaskAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/workers/tasks/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || r.Method != http.MethodPost {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	id, action := parts[0], parts[1]
	var req struct {
		WorkerID   *string `json:"worker_id"`
		ClaimEpoch *uint64 `json:"claim_epoch"`
		Output     any     `json:"output"`
		Message    string  `json:"message"`
		Retryable  bool    `json:"retryable"`
		Checkpoint any     `json:"checkpoint"`
		CheckSeq   *uint64 `json:"checkpoint_seq"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WorkerID == nil || req.ClaimEpoch == nil {
		writeErr(w, http.StatusUnprocessableEntity, "worker_id and claim_epoch are required")
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.tasks[id]
	if !ok {
		writeErr(w, http.StatusNotFound, "worker_task "+id)
		return
	}
	sameLease := rec.WorkerID == *req.WorkerID && rec.ClaimEpoch == *req.ClaimEpoch
	switch action {
	case "complete":
		if rec.State == StateCompleted && sameLease {
			writeJSON(w, http.StatusOK, nil) // idempotent retry
			return
		}
		if rec.State != StateClaimed || !sameLease {
			rec.StaleRejections++
			writeErr(w, http.StatusConflict, "worker task lease changed")
			return
		}
		if req.Output == nil {
			req.Output = map[string]any{}
		}
		rec.State = StateCompleted
		rec.Output = req.Output
		rec.CompletedAt = e.now.Format(time.RFC3339Nano)
	case "fail":
		if rec.State != StateClaimed || !sameLease {
			rec.StaleRejections++
			writeErr(w, http.StatusConflict, "worker task lease changed")
			return
		}
		rec.FailMessage = req.Message
		rec.FailRetryable = req.Retryable
		rec.ErrorMessage = req.Message
		retryable := req.Retryable
		rec.ErrorRetryable = &retryable
		if job := e.jobForLocked(rec); job != nil && req.Retryable && rec.Attempt+1 < job.maxAttempts {
			rec.State = StatePending
			rec.WorkerID = ""
			rec.Attempt++
		} else {
			rec.State = StateFailed
			rec.CompletedAt = e.now.Format(time.RFC3339Nano)
		}
	case "heartbeat":
		if rec.State != StateClaimed || !sameLease {
			rec.StaleRejections++
			writeErr(w, http.StatusConflict, "worker task lease changed")
			return
		}
		rec.Heartbeats++
		rec.HeartbeatAt = e.now.Format(time.RFC3339Nano)
		if req.Checkpoint != nil {
			if req.CheckSeq == nil || *req.CheckSeq != rec.CheckpointSeq {
				writeErr(w, http.StatusConflict, "stale checkpoint sequence")
				return
			}
			rec.ResumeCheckpoint = req.Checkpoint
			rec.CheckpointSeq++
		}
		writeJSON(w, http.StatusOK, map[string]any{"checkpoint_seq": rec.CheckpointSeq})
		return
	default:
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	e.syncJobLocked(rec)
	e.notifyLocked()
	writeJSON(w, http.StatusOK, nil)
}

// ---------------------------------------------------------------------------
// Instances
// ---------------------------------------------------------------------------

// Instance returns a snapshot of an instance.
func (e *Engine) Instance(id string) (orch8.TaskInstance, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	inst, ok := e.instances[id]
	if !ok {
		return orch8.TaskInstance{}, false
	}
	return *inst, true
}

// SetInstanceState changes an instance's state (e.g. to "completed").
func (e *Engine) SetInstanceState(id, state string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if inst, ok := e.instances[id]; ok {
		inst.State = state
		inst.UpdatedAt = e.now.Format(time.RFC3339Nano)
		e.notifyLocked()
	}
}

func (e *Engine) createInstanceLocked(body map[string]any) *orch8.TaskInstance {
	str := func(k string) string { s, _ := body[k].(string); return s }
	id := str("id")
	if id == "" {
		id = newID()
	}
	now := e.now.Format(time.RFC3339Nano)
	inst := &orch8.TaskInstance{
		ID: id, SequenceID: str("sequence_id"), TenantID: str("tenant_id"), Namespace: str("namespace"),
		State: "scheduled", Timezone: str("timezone"), Metadata: body["metadata"], Context: body["context"],
		IdempotencyKey: str("idempotency_key"), CreatedAt: now, UpdatedAt: now,
	}
	if inst.Timezone == "" {
		inst.Timezone = "UTC"
	}
	e.instances[id] = inst
	return inst
}

func (e *Engine) handleInstances(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		e.mu.Lock()
		inst := *e.createInstanceLocked(body)
		e.notifyLocked()
		e.mu.Unlock()
		writeJSON(w, http.StatusCreated, inst)
	case http.MethodGet:
		e.mu.Lock()
		out := make([]orch8.TaskInstance, 0, len(e.instances))
		state := r.URL.Query().Get("state")
		for _, inst := range e.instances {
			if state == "" || inst.State == state {
				out = append(out, *inst)
			}
		}
		e.mu.Unlock()
		sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt < out[j].CreatedAt })
		writeJSON(w, http.StatusOK, out)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (e *Engine) handleInstance(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/instances/")
	parts := strings.Split(rest, "/")
	id := parts[0]
	e.mu.Lock()
	defer e.mu.Unlock()
	inst, ok := e.instances[id]
	if !ok {
		writeErr(w, http.StatusNotFound, "instance "+id)
		return
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, inst)
	case len(parts) == 2 && parts[1] == "state" && r.Method == http.MethodPatch:
		var body struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.State == "" {
			writeErr(w, http.StatusUnprocessableEntity, "state is required")
			return
		}
		inst.State = body.State
		inst.UpdatedAt = e.now.Format(time.RFC3339Nano)
		e.notifyLocked()
		writeJSON(w, http.StatusOK, inst)
	default:
		writeErr(w, http.StatusNotFound, "not found")
	}
}
