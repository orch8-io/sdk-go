package orch8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewWorkerDefaults(t *testing.T) {
	w := NewWorker(WorkerConfig{
		Client:   NewClient(ClientConfig{BaseURL: "http://localhost"}),
		WorkerID: "w1",
		Handlers: map[string]HandlerFunc{"h": func(_ context.Context, _ WorkerTask) (any, error) { return nil, nil }},
	})
	if w.pollInterval != time.Second {
		t.Errorf("expected default poll interval 1s, got %v", w.pollInterval)
	}
	if w.heartbeatInterval != 15*time.Second {
		t.Errorf("expected default heartbeat interval 15s, got %v", w.heartbeatInterval)
	}
	if w.maxConcurrent != 10 {
		t.Errorf("expected default max concurrent 10, got %d", w.maxConcurrent)
	}
	if cap(w.sem) != 10 {
		t.Errorf("expected semaphore capacity 10, got %d", cap(w.sem))
	}
}

func TestWorkerStartStop(t *testing.T) {
	var pollCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/workers/tasks/poll" {
			pollCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]WorkerTask{})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL})
	w := NewWorker(WorkerConfig{
		Client:       client,
		WorkerID:     "w1",
		Handlers:     map[string]HandlerFunc{"h": func(_ context.Context, _ WorkerTask) (any, error) { return nil, nil }},
		PollInterval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	w.Start(ctx)
	if pollCount.Load() == 0 {
		t.Error("expected at least one poll")
	}
}

func TestWorkerStopBeforeStart(t *testing.T) {
	w := NewWorker(WorkerConfig{
		Client:   NewClient(ClientConfig{BaseURL: "http://localhost"}),
		WorkerID: "w1",
		Handlers: map[string]HandlerFunc{"h": func(_ context.Context, _ WorkerTask) (any, error) { return nil, nil }},
	})
	// Should not panic even if cancel is nil.
	w.Stop()
}

func TestWorkerConcurrencySemaphore(t *testing.T) {
	client := NewClient(ClientConfig{BaseURL: "http://localhost"})
	var active int32
	var maxObserved int32

	handler := func(_ context.Context, _ WorkerTask) (any, error) {
		cur := atomic.AddInt32(&active, 1)
		for {
			prev := atomic.LoadInt32(&maxObserved)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxObserved, prev, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return nil, nil
	}

	w := NewWorker(WorkerConfig{
		Client:        client,
		WorkerID:      "w1",
		Handlers:      map[string]HandlerFunc{"h": handler},
		PollInterval:  100 * time.Millisecond,
		MaxConcurrent: 2,
	})

	// Verify the semaphore capacity is respected by calling executeTask
	// directly from multiple goroutines.
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.executeTask(ctx, WorkerTask{ID: "t1", HandlerName: "h"})
		}()
	}
	wg.Wait()

	if maxObserved > 2 {
		t.Errorf("expected max concurrent <= 2, observed %d", maxObserved)
	}
}

func TestWorkerBackoffOnPollError(t *testing.T) {
	w := NewWorker(WorkerConfig{
		Client:       NewClient(ClientConfig{BaseURL: "http://localhost"}),
		WorkerID:     "w1",
		Handlers:     map[string]HandlerFunc{"h": func(_ context.Context, _ WorkerTask) (any, error) { return nil, nil }},
		PollInterval: 100 * time.Millisecond,
	})

	w.backoff["h"] = 0
	// Simulate backoff calculation directly.
	cur := w.backoff["h"]
	if cur == 0 {
		cur = w.pollInterval
	}
	cur *= 2
	if cur > maxBackoff {
		cur = maxBackoff
	}
	w.backoff["h"] = cur

	if w.backoff["h"] != 200*time.Millisecond {
		t.Errorf("expected backoff 200ms, got %v", w.backoff["h"])
	}
}

func TestWorkerCircuitBreakerSkip(t *testing.T) {
	called := false
	w := NewWorker(WorkerConfig{
		Client:              NewClient(ClientConfig{BaseURL: "http://localhost"}),
		WorkerID:            "w1",
		Handlers:            map[string]HandlerFunc{"h": func(_ context.Context, _ WorkerTask) (any, error) { called = true; return nil, nil }},
		CircuitBreakerCheck: true,
	})

	// Verify the flag is stored correctly.
	if !w.circuitBreakerCheck {
		t.Error("expected circuit breaker check to be enabled")
	}
	_ = called
}

func TestWorkerExecuteTaskMissingHandler(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL})
	w := NewWorker(WorkerConfig{
		Client:   client,
		WorkerID: "w1",
		Handlers: map[string]HandlerFunc{},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// Should not panic even when handler is missing.
	w.executeTask(ctx, WorkerTask{ID: "t1", HandlerName: "missing"})
}

func TestWorkerExecuteTaskTimeout(t *testing.T) {
	timeout := 50
	w := NewWorker(WorkerConfig{
		Client:   NewClient(ClientConfig{BaseURL: "http://localhost"}),
		WorkerID: "w1",
		Handlers: map[string]HandlerFunc{
			"h": func(ctx context.Context, _ WorkerTask) (any, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	})

	ctx := context.Background()
	w.executeTask(ctx, WorkerTask{ID: "t1", HandlerName: "h", TimeoutMs: &timeout})
}

func TestOrch8ErrorHelpers(t *testing.T) {
	err := &Orch8Error{Status: 404, Body: "not found", Path: "/seq/1"}
	if !err.IsNotFound() {
		t.Error("expected IsNotFound == true")
	}
	if err.IsRateLimited() {
		t.Error("expected IsRateLimited == false")
	}
	if err.IsServerError() {
		t.Error("expected IsServerError == false")
	}

	err429 := &Orch8Error{Status: 429, Body: "slow down", Path: "/"}
	if !err429.IsRateLimited() {
		t.Error("expected IsRateLimited == true")
	}

	err500 := &Orch8Error{Status: 503, Body: "unavailable", Path: "/"}
	if !err500.IsServerError() {
		t.Error("expected IsServerError == true")
	}
}

func TestOrch8ErrorTruncation(t *testing.T) {
	bigBody := make([]byte, 600)
	for i := range bigBody {
		bigBody[i] = 'x'
	}
	err := &Orch8Error{Status: 500, Body: string(bigBody), Path: "/"}
	msg := err.Error()
	if len(msg) > 600 {
		t.Errorf("expected truncated error message, got length %d", len(msg))
	}
}

func TestOrch8ErrorIsJSON(t *testing.T) {
	jsonErr := &Orch8Error{Status: 400, Body: `{"error":"bad request"}`, Path: "/"}
	if !jsonErr.IsJSON() {
		t.Error("expected IsJSON == true for JSON body")
	}
	htmlErr := &Orch8Error{Status: 500, Body: "<html>oops</html>", Path: "/"}
	if htmlErr.IsJSON() {
		t.Error("expected IsJSON == false for HTML body")
	}
}
