package orch8

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestJobsEnqueueBody(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(Job{ID: "j1", InstanceID: "i1", Handler: "email", Status: JobScheduled})
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL})
	runAt := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	maxB := int64(60000)
	job, err := c.Jobs.Enqueue(context.Background(), EnqueueJobRequest{
		Handler: "email", Payload: map[string]any{"to": "a@b.c"}, Queue: "q", Priority: Ptr(5),
		Retry: &JobRetry{MaxAttempts: 3, InitialBackoffMs: 1000, MaxBackoffMs: &maxB},
		RunAt: &runAt, IdempotencyKey: "k1", Metadata: map[string]any{"src": "test"},
	})
	if err != nil || job.ID != "j1" || job.Status != JobScheduled {
		t.Fatal(job, err)
	}
	retry := body["retry"].(map[string]any)
	if body["handler"] != "email" || body["run_at"] != "2026-10-01T09:00:00Z" || body["idempotency_key"] != "k1" ||
		body["priority"] != float64(5) || retry["max_backoff_ms"] != float64(60000) || body["delay_ms"] != nil {
		t.Fatalf("unexpected body %#v", body)
	}
	if _, err := c.Jobs.Enqueue(context.Background(), EnqueueJobRequest{Handler: "x", DelayMs: Ptr(int64(1)), RunAt: &runAt}); err == nil {
		t.Fatal("expected delay/run_at conflict error")
	}
}

func TestJobsListIteratesCursors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("status") != "failed" || q.Get("handler") != "h" || q.Get("limit") != "2" {
			t.Errorf("unexpected query %s", r.URL.RawQuery)
		}
		switch q.Get("cursor") {
		case "":
			_ = json.NewEncoder(w).Encode(JobPage{Items: []Job{{ID: "1"}, {ID: "2"}}, NextCursor: "c2"})
		case "c2":
			_ = json.NewEncoder(w).Encode(JobPage{Items: []Job{{ID: "3"}}})
		}
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL})
	it := c.Jobs.List(context.Background(), JobListOptions{Handler: "h", Status: JobFailed, Limit: 2})
	var ids []string
	for it.Next() {
		ids = append(ids, it.Job().ID)
	}
	if it.Err() != nil || len(ids) != 3 || ids[2] != "3" {
		t.Fatal(ids, it.Err())
	}
}

func TestJobsWaitForAndCancel(t *testing.T) {
	var gets atomic.Int32
	var deleted atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/jobs/j1" {
			deleted.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		status := JobRunning
		if gets.Add(1) >= 3 {
			status = JobCompleted
		}
		_ = json.NewEncoder(w).Encode(Job{ID: "j1", Status: status, Output: map[string]any{"ok": true}})
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{BaseURL: srv.URL})
	job, err := c.Jobs.WaitFor(context.Background(), "j1", WaitOptions{Interval: time.Millisecond})
	if err != nil || job.Status != JobCompleted || gets.Load() != 3 {
		t.Fatal(job, err, gets.Load())
	}
	gets.Store(-1000)
	_, err = c.Jobs.WaitFor(context.Background(), "j1", WaitOptions{Interval: time.Millisecond, Timeout: 20 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if err := c.Jobs.Cancel(context.Background(), "j1"); err != nil || !deleted.Load() {
		t.Fatal(err)
	}
}
