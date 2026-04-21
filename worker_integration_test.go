package orch8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerIntegration(t *testing.T) {
	var polls atomic.Int32
	var completes atomic.Int32
	var heartbeats atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workers/tasks/poll":
			polls.Add(1)
			if polls.Load() == 1 {
				// Return a task on first poll.
				json.NewEncoder(w).Encode([]WorkerTask{
					{
						ID:          "wt-e2e-1",
						InstanceID:  "inst-1",
						BlockID:     "b-1",
						HandlerName: "e2e-handler",
						CreatedAt:   "2025-01-01T00:00:00Z",
					},
				})
				return
			}
			// Empty on subsequent polls.
			w.Write([]byte("[]"))
		case "/workers/tasks/wt-e2e-1/complete":
			completes.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case "/workers/tasks/wt-e2e-1/heartbeat":
			heartbeats.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL})
	var handled atomic.Bool

	w := NewWorker(WorkerConfig{
		Client:       client,
		WorkerID:     "w-e2e",
		Handlers:     map[string]HandlerFunc{
			"e2e-handler": func(_ context.Context, _ WorkerTask) (any, error) {
				handled.Store(true)
				time.Sleep(80 * time.Millisecond) // stay alive long enough for heartbeat
				return map[string]any{"done": true}, nil
			},
		},
		PollInterval:      50 * time.Millisecond,
		HeartbeatInterval: 50 * time.Millisecond,
		MaxConcurrent:     5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	w.Start(ctx)

	if !handled.Load() {
		t.Error("expected handler to be called")
	}
	if completes.Load() == 0 {
		t.Error("expected completeTask to be called")
	}
	if heartbeats.Load() == 0 {
		t.Error("expected at least one heartbeat")
	}
}

func TestWorkerIntegrationHandlerFailure(t *testing.T) {
	var fails atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workers/tasks/poll":
			json.NewEncoder(w).Encode([]WorkerTask{
				{
					ID:          "wt-fail-1",
					InstanceID:  "inst-1",
					BlockID:     "b-1",
					HandlerName: "fail-handler",
					CreatedAt:   "2025-01-01T00:00:00Z",
				},
			})
		case "/workers/tasks/wt-fail-1/fail":
			fails.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Write([]byte("[]"))
		}
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL})
	w := NewWorker(WorkerConfig{
		Client:   client,
		WorkerID: "w-fail",
		Handlers: map[string]HandlerFunc{
			"fail-handler": func(_ context.Context, _ WorkerTask) (any, error) {
				return nil, errorf("intentional failure")
			},
		},
		PollInterval: 50 * time.Millisecond,
		MaxConcurrent: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	w.Start(ctx)

	if fails.Load() == 0 {
		t.Error("expected failTask to be called")
	}
}

func TestWorkerIntegrationCircuitBreaker(t *testing.T) {
	var polls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/circuit-breakers/skip-handler":
			json.NewEncoder(w).Encode(CircuitBreakerState{Handler: "skip-handler", State: "open"})
		case "/workers/tasks/poll":
			polls.Add(1)
			w.Write([]byte("[]"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := NewClient(ClientConfig{BaseURL: srv.URL})
	w := NewWorker(WorkerConfig{
		Client:              client,
		WorkerID:            "w-cb",
		Handlers:            map[string]HandlerFunc{"skip-handler": func(_ context.Context, _ WorkerTask) (any, error) { return nil, nil }},
		PollInterval:        50 * time.Millisecond,
		CircuitBreakerCheck: true,
		MaxConcurrent:       5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	w.Start(ctx)

	if polls.Load() > 0 {
		t.Error("expected zero polls when circuit breaker is open")
	}
}

// errorf is a tiny helper to avoid importing fmt just for this test.
func errorf(msg string) error {
	return &testError{msg}
}

type testError struct{ msg string }
func (e *testError) Error() string { return e.msg }
