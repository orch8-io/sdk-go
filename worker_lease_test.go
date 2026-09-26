package orch8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestDecodePollBatchAcceptsEnvelopeAndLegacyArray(t *testing.T) {
	batch, err := DecodePollBatch(json.RawMessage(`{"tasks":[{"id":"t1","claim_epoch":4}],"lease_secs":30,"heartbeat_interval_secs":10,"poll_after_ms":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Tasks) != 1 || batch.Tasks[0].ClaimEpoch != 4 || *batch.LeaseSecs != 30 || *batch.HeartbeatIntervalSecs != 10 || *batch.PollAfterMs != 0 {
		t.Fatalf("unexpected batch: %+v", batch)
	}
	legacy, err := DecodePollBatch(json.RawMessage(`[{"id":"t2"}]`))
	if err != nil || len(legacy.Tasks) != 1 || legacy.LeaseSecs != nil {
		t.Fatalf("legacy decode: %+v %v", legacy, err)
	}
	if _, err := DecodePollBatch(json.RawMessage(`{"nope":1}`)); err == nil {
		t.Fatal("expected error for missing tasks array")
	}
}

func TestWorkerEchoesClaimEpochOnAcknowledgements(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	served := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/workers/tasks/poll":
			mu.Lock()
			first := !served
			served = true
			mu.Unlock()
			tasks := []WorkerTask{}
			if first {
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req["handler_name"] == "ok" {
					tasks = append(tasks, WorkerTask{ID: "t-ok", HandlerName: "ok", ClaimEpoch: 7})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks, "lease_secs": 2, "heartbeat_interval_secs": 1, "poll_after_ms": 0})
		default:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			bodies[r.URL.Path] = body
			mu.Unlock()
			_, _ = w.Write([]byte(`{"checkpoint_seq":0}`))
		}
	}))
	defer srv.Close()

	completed := make(chan struct{})
	w := NewWorker(WorkerConfig{
		Client:       NewClient(ClientConfig{BaseURL: srv.URL}),
		WorkerID:     "w1",
		PollInterval: 20 * time.Millisecond,
		Handlers: map[string]HandlerFunc{
			"ok": func(ctx context.Context, task WorkerTask) (any, error) {
				time.Sleep(1200 * time.Millisecond) // long enough for one lease-derived heartbeat (1s)
				return map[string]any{"done": true}, nil
			},
		},
		OnTaskComplete: func(WorkerTask, any) { close(completed) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)
	select {
	case <-completed:
	case <-time.After(5 * time.Second):
		t.Fatal("task never completed")
	}
	cancel()

	mu.Lock()
	defer mu.Unlock()
	complete := bodies["/workers/tasks/t-ok/complete"]
	if complete["claim_epoch"] != float64(7) || complete["worker_id"] != "w1" {
		t.Fatalf("complete body missing lease: %#v", complete)
	}
	hb := bodies["/workers/tasks/t-ok/heartbeat"]
	if hb == nil || hb["claim_epoch"] != float64(7) {
		t.Fatalf("heartbeat body missing lease (interval should follow lease hints): %#v", hb)
	}
}

func TestWorkerDoesNotReportSuccessWhenCompletionRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"worker task lease changed"}`))
	}))
	defer srv.Close()
	called := false
	failed := false
	w := NewWorker(WorkerConfig{
		Client:         NewClient(ClientConfig{BaseURL: srv.URL}),
		WorkerID:       "w1",
		Handlers:       map[string]HandlerFunc{"h": func(context.Context, WorkerTask) (any, error) { return nil, nil }},
		OnTaskComplete: func(WorkerTask, any) { called = true },
		OnTaskFail:     func(WorkerTask, error) { failed = true },
	})
	w.executeTask(context.Background(), WorkerTask{ID: "t1", HandlerName: "h", ClaimEpoch: 2})
	if called || failed {
		t.Fatalf("rejected ack must not fire callbacks: complete=%v fail=%v", called, failed)
	}
}
