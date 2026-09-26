package orch8_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	orch8 "github.com/orch8-io/sdk-go"
	"github.com/orch8-io/sdk-go/orch8test"
)

func pushHandler(t *testing.T, eng *orch8test.Engine, opts orch8.PushHandlerOptions) http.Handler {
	t.Helper()
	h, err := orch8.NewPushHandler(eng.Client(), map[string]orch8.HandlerFunc{
		"resize": func(_ context.Context, task orch8.WorkerTask) (any, error) {
			return map[string]any{"resized": task.Params.(map[string]any)["w"]}, nil
		},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func doPush(h http.Handler, body []byte, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/push", bytes.NewReader(body))
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPushHandlerClaimsRunsAndCompletes(t *testing.T) {
	eng := orch8test.NewEngine(t)
	task := eng.EnqueueTask("resize", map[string]any{"w": 640}, orch8test.WithQueue("images"))
	h := pushHandler(t, eng, orch8.PushHandlerOptions{Secret: "shh", WorkerID: "push-1"})
	body := eng.PushEnvelope(task.ID)

	resp := doPush(h, body, orch8test.SignedPushHeaders("shh", body))
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body)
	}
	var out orch8.PushResult
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	if len(out.Claimed) != 1 || out.Claimed[0] != task.ID {
		t.Fatalf("unexpected result %s", resp.Body)
	}
	rec, _ := eng.Task(task.ID)
	if rec.State != orch8test.StateCompleted || rec.WorkerID != "push-1" || rec.Output.(map[string]any)["resized"] != float64(640) {
		t.Fatalf("unexpected record %+v", rec)
	}

	// A duplicate push after completion claims nothing and still acks 200.
	resp = doPush(h, body, orch8test.SignedPushHeaders("shh", body))
	if resp.Code != http.StatusOK {
		t.Fatalf("duplicate push status %d", resp.Code)
	}
}

func TestPushHandlerRejections(t *testing.T) {
	eng := orch8test.NewEngine(t)
	task := eng.EnqueueTask("resize", map[string]any{"w": 1}, orch8test.WithQueue("images"))
	h := pushHandler(t, eng, orch8.PushHandlerOptions{Secret: "shh"})
	body := eng.PushEnvelope(task.ID)

	if resp := doPush(h, body, orch8test.SignedPushHeaders("wrong", body)); resp.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature: %d", resp.Code)
	}
	if resp := doPush(h, body, http.Header{}); resp.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned: %d", resp.Code)
	}
	garbage := []byte(`not json`)
	if resp := doPush(h, garbage, orch8test.SignedPushHeaders("shh", garbage)); resp.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", resp.Code)
	}
	unknown := []byte(`{"task_id":"x","handler_name":"nope","queue_name":"images"}`)
	if resp := doPush(h, unknown, orch8test.SignedPushHeaders("shh", unknown)); resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown handler: %d", resp.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/push", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
	if r, _ := eng.Task(task.ID); r.State != orch8test.StatePending {
		t.Fatalf("rejected pushes must not claim: %s", r.State)
	}
}

func TestPushHandlerAsyncAndUnsignedOptIn(t *testing.T) {
	eng := orch8test.NewEngine(t)
	task := eng.EnqueueTask("resize", map[string]any{"w": 2}, orch8test.WithQueue("images"))
	h := pushHandler(t, eng, orch8.PushHandlerOptions{AllowUnsigned: true, Async: true})
	resp := doPush(h, eng.PushEnvelope(task.ID), http.Header{})
	if resp.Code != http.StatusAccepted {
		t.Fatalf("async status %d", resp.Code)
	}
	if rec := eng.WaitForTask(t, task.ID, 5*time.Second); rec.State != orch8test.StateCompleted {
		t.Fatalf("async push not completed: %+v", rec)
	}
}
