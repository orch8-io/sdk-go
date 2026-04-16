package orch8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func jsonHandler(t *testing.T, method, path string, status int, response any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			t.Errorf("expected method %s, got %s", method, r.Method)
		}
		if r.URL.Path != path {
			t.Errorf("expected path %s, got %s", path, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if response != nil {
			json.NewEncoder(w).Encode(response)
		}
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestHealth(t *testing.T) {
	var gotPath, gotTenantID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTenantID = r.Header.Get("X-Tenant-Id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL, TenantID: "t1"})
	resp, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/health/ready" {
		t.Errorf("expected path /health/ready, got %s", gotPath)
	}
	if gotTenantID != "t1" {
		t.Errorf("expected X-Tenant-Id t1, got %s", gotTenantID)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status ok, got %s", resp.Status)
	}
}

// ---------------------------------------------------------------------------
// X-Tenant-Id header
// ---------------------------------------------------------------------------

func TestTenantIDHeader(t *testing.T) {
	var gotTenantID string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotTenantID = r.Header.Get("X-Tenant-Id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL, TenantID: "tenant-xyz"})
	_, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTenantID != "tenant-xyz" {
		t.Errorf("expected X-Tenant-Id tenant-xyz, got %s", gotTenantID)
	}
}

func TestNoTenantIDHeaderWhenEmpty(t *testing.T) {
	var gotTenantID string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotTenantID = r.Header.Get("X-Tenant-Id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	_, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotTenantID != "" {
		t.Errorf("expected no X-Tenant-Id header, got %s", gotTenantID)
	}
}

// ---------------------------------------------------------------------------
// Error handling
// ---------------------------------------------------------------------------

func TestErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	_, err := c.GetInstance(context.Background(), "missing-id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	oe, ok := err.(*Orch8Error)
	if !ok {
		t.Fatalf("expected *Orch8Error, got %T", err)
	}
	if oe.Status != 404 {
		t.Errorf("expected status 404, got %d", oe.Status)
	}
	if oe.Path != "/instances/missing-id" {
		t.Errorf("expected path /instances/missing-id, got %s", oe.Path)
	}
}

func TestDeleteReturnsNilFor204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeleteCron(context.Background(), "cron-1")
	if err != nil {
		t.Fatalf("expected nil error for 204, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sequences
// ---------------------------------------------------------------------------

func TestCreateSequence(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SequenceDefinition{
			ID:   "seq-1",
			Name: "test-seq",
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	body := map[string]any{"name": "test-seq", "blocks": []any{}}
	resp, err := c.CreateSequence(context.Background(), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/sequences" {
		t.Errorf("expected path /sequences, got %s", gotPath)
	}
	if resp.ID != "seq-1" {
		t.Errorf("expected ID seq-1, got %s", resp.ID)
	}
	if gotBody["name"] != "test-seq" {
		t.Errorf("expected body name test-seq, got %v", gotBody["name"])
	}
}

func TestGetSequence(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/sequences/seq-abc" {
			t.Errorf("expected path /sequences/seq-abc, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SequenceDefinition{ID: "seq-abc", Name: "my-seq"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetSequence(context.Background(), "seq-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "seq-abc" {
		t.Errorf("expected ID seq-abc, got %s", resp.ID)
	}
	if resp.Name != "my-seq" {
		t.Errorf("expected Name my-seq, got %s", resp.Name)
	}
}

func TestGetSequenceByName(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/sequences/by-name" {
			t.Errorf("expected path /sequences/by-name, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("tenant_id") != "t1" {
			t.Errorf("expected tenant_id t1, got %s", q.Get("tenant_id"))
		}
		if q.Get("namespace") != "ns1" {
			t.Errorf("expected namespace ns1, got %s", q.Get("namespace"))
		}
		if q.Get("name") != "my-seq" {
			t.Errorf("expected name my-seq, got %s", q.Get("name"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SequenceDefinition{ID: "seq-xyz", Name: "my-seq"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetSequenceByName(context.Background(), "t1", "ns1", "my-seq", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "seq-xyz" {
		t.Errorf("expected ID seq-xyz, got %s", resp.ID)
	}
}

func TestGetSequenceByNameWithVersion(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("version") != "3" {
			t.Errorf("expected version 3, got %s", q.Get("version"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SequenceDefinition{ID: "seq-v3", Version: 3})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	v := 3
	resp, err := c.GetSequenceByName(context.Background(), "t1", "ns1", "my-seq", &v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Version != 3 {
		t.Errorf("expected Version 3, got %d", resp.Version)
	}
}

func TestDeprecateSequence(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/sequences/seq-1/deprecate" {
			t.Errorf("expected path /sequences/seq-1/deprecate, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeprecateSequence(context.Background(), "seq-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListSequenceVersions(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/sequences/versions" {
			t.Errorf("expected path /sequences/versions, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("tenant_id") != "t1" {
			t.Errorf("expected tenant_id t1, got %s", q.Get("tenant_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]SequenceDefinition{
			{ID: "seq-v1", Version: 1},
			{ID: "seq-v2", Version: 2},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListSequenceVersions(context.Background(), "t1", "ns1", "my-seq")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(result))
	}
	if result[0].Version != 1 {
		t.Errorf("expected version 1, got %d", result[0].Version)
	}
}

// ---------------------------------------------------------------------------
// Instances
// ---------------------------------------------------------------------------

func TestCreateInstance(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/instances" {
			t.Errorf("expected path /instances, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TaskInstance{ID: "inst-1", State: "pending"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreateInstance(context.Background(), map[string]any{"sequence_id": "seq-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "inst-1" {
		t.Errorf("expected ID inst-1, got %s", resp.ID)
	}
	if resp.State != "pending" {
		t.Errorf("expected State pending, got %s", resp.State)
	}
}

func TestBatchCreateInstances(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/instances/batch" {
			t.Errorf("expected path /instances/batch, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BatchCreateResponse{Created: 5})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.BatchCreateInstances(context.Background(), map[string]any{"instances": []any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Created != 5 {
		t.Errorf("expected Created 5, got %d", resp.Created)
	}
}

func TestGetInstance(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-42" {
			t.Errorf("expected path /instances/inst-42, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TaskInstance{ID: "inst-42", State: "running"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetInstance(context.Background(), "inst-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "inst-42" {
		t.Errorf("expected ID inst-42, got %s", resp.ID)
	}
}

func TestListInstances(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances" {
			t.Errorf("expected path /instances, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TaskInstance{
			{ID: "inst-1"},
			{ID: "inst-2"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListInstances(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(result))
	}
}

func TestListInstancesWithFilter(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != "running" {
			t.Errorf("expected state running, got %s", q.Get("state"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TaskInstance{{ID: "inst-1", State: "running"}})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListInstances(context.Background(), map[string]string{"state": "running"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(result))
	}
	if result[0].State != "running" {
		t.Errorf("expected State running, got %s", result[0].State)
	}
}

func TestUpdateInstanceState(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/state" {
			t.Errorf("expected path /instances/inst-1/state, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.UpdateInstanceState(context.Background(), "inst-1", map[string]string{"state": "paused"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateInstanceContext(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/context" {
			t.Errorf("expected path /instances/inst-1/context, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.UpdateInstanceContext(context.Background(), "inst-1", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendSignal(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/signals" {
			t.Errorf("expected path /instances/inst-1/signals, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SignalResponse{SignalID: "sig-abc"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.SendSignal(context.Background(), "inst-1", map[string]any{"event": "approved"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SignalID != "sig-abc" {
		t.Errorf("expected signal_id sig-abc, got %s", resp.SignalID)
	}
}

func TestGetOutputs(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/outputs" {
			t.Errorf("expected path /instances/inst-1/outputs, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]StepOutput{
			{ID: "out-1", BlockID: "block-a"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.GetOutputs(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 output, got %d", len(result))
	}
	if result[0].ID != "out-1" {
		t.Errorf("expected ID out-1, got %s", result[0].ID)
	}
}

func TestGetExecutionTree(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/tree" {
			t.Errorf("expected path /instances/inst-1/tree, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ExecutionNode{
			{ID: "node-1", BlockType: "step", State: "completed"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.GetExecutionTree(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].ID != "node-1" {
		t.Errorf("expected ID node-1, got %s", result[0].ID)
	}
}

func TestRetryInstance(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/retry" {
			t.Errorf("expected path /instances/inst-1/retry, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TaskInstance{ID: "inst-1", State: "pending"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.RetryInstance(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "inst-1" {
		t.Errorf("expected ID inst-1, got %s", resp.ID)
	}
}

func TestListCheckpoints(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/checkpoints" {
			t.Errorf("expected path /instances/inst-1/checkpoints, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]Checkpoint{
			{ID: "ckpt-1", InstanceID: "inst-1"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListCheckpoints(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(result))
	}
	if result[0].ID != "ckpt-1" {
		t.Errorf("expected ID ckpt-1, got %s", result[0].ID)
	}
}

func TestSaveCheckpoint(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/checkpoints" {
			t.Errorf("expected path /instances/inst-1/checkpoints, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Checkpoint{ID: "ckpt-new", InstanceID: "inst-1"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.SaveCheckpoint(context.Background(), "inst-1", map[string]any{"data": "state"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "ckpt-new" {
		t.Errorf("expected ID ckpt-new, got %s", resp.ID)
	}
}

func TestGetLatestCheckpoint(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/checkpoints/latest" {
			t.Errorf("expected path /instances/inst-1/checkpoints/latest, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Checkpoint{ID: "ckpt-latest", InstanceID: "inst-1"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetLatestCheckpoint(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "ckpt-latest" {
		t.Errorf("expected ID ckpt-latest, got %s", resp.ID)
	}
}

func TestPruneCheckpoints(t *testing.T) {
	var gotBody map[string]any
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/checkpoints/prune" {
			t.Errorf("expected path /instances/inst-1/checkpoints/prune, got %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	keep := 5
	err := c.PruneCheckpoints(context.Background(), "inst-1", &keep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v, ok := gotBody["keep"].(float64); !ok || int(v) != 5 {
		t.Errorf("expected body keep=5, got %v", gotBody["keep"])
	}
}

func TestListAuditLog(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/audit" {
			t.Errorf("expected path /instances/inst-1/audit, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AuditEntry{
			{Timestamp: "2024-01-01T00:00:00Z", Event: "state_changed"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListAuditLog(context.Background(), "inst-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Event != "state_changed" {
		t.Errorf("expected event state_changed, got %s", result[0].Event)
	}
}

func TestBulkUpdateState(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/instances/bulk/state" {
			t.Errorf("expected path /instances/bulk/state, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BulkResponse{Updated: 3})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.BulkUpdateState(context.Background(), map[string]any{"tenant_id": "t1"}, "paused")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Updated != 3 {
		t.Errorf("expected Updated 3, got %d", resp.Updated)
	}
}

func TestBulkReschedule(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/instances/bulk/reschedule" {
			t.Errorf("expected path /instances/bulk/reschedule, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BulkResponse{Updated: 7})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.BulkReschedule(context.Background(), map[string]any{"state": "failed"}, 60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Updated != 7 {
		t.Errorf("expected Updated 7, got %d", resp.Updated)
	}
}

func TestListDLQ(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/instances/dlq" {
			t.Errorf("expected path /instances/dlq, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TaskInstance{
			{ID: "dlq-inst-1", State: "failed"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListDLQ(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(result))
	}
	if result[0].ID != "dlq-inst-1" {
		t.Errorf("expected ID dlq-inst-1, got %s", result[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Cron
// ---------------------------------------------------------------------------

func TestCreateCron(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/cron" {
			t.Errorf("expected path /cron, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CronSchedule{ID: "cron-1", CronExpr: "0 * * * *"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreateCron(context.Background(), map[string]any{"cron_expr": "0 * * * *"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "cron-1" {
		t.Errorf("expected ID cron-1, got %s", resp.ID)
	}
	if resp.CronExpr != "0 * * * *" {
		t.Errorf("expected CronExpr 0 * * * *, got %s", resp.CronExpr)
	}
}

func TestListCronNoTenantID(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/cron" {
			t.Errorf("expected path /cron, got %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Errorf("expected no query params, got %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]CronSchedule{{ID: "cron-1"}})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListCron(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(result))
	}
}

func TestListCronWithTenantID(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tenant_id") != "t1" {
			t.Errorf("expected tenant_id t1, got %s", r.URL.Query().Get("tenant_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]CronSchedule{{ID: "cron-2", TenantID: "t1"}})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListCron(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 || result[0].TenantID != "t1" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestGetCron(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/cron/cron-42" {
			t.Errorf("expected path /cron/cron-42, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CronSchedule{ID: "cron-42", CronExpr: "*/5 * * * *"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetCron(context.Background(), "cron-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "cron-42" {
		t.Errorf("expected ID cron-42, got %s", resp.ID)
	}
}

func TestUpdateCron(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/cron/cron-1" {
			t.Errorf("expected path /cron/cron-1, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CronSchedule{ID: "cron-1", CronExpr: "0 12 * * *"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.UpdateCron(context.Background(), "cron-1", map[string]any{"cron_expr": "0 12 * * *"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CronExpr != "0 12 * * *" {
		t.Errorf("expected CronExpr 0 12 * * *, got %s", resp.CronExpr)
	}
}

// ---------------------------------------------------------------------------
// Triggers
// ---------------------------------------------------------------------------

func TestFireTrigger(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FireTriggerResponse{
			InstanceID:   "inst-1",
			Trigger:      "my-trigger",
			SequenceName: "my-seq",
		})
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.FireTrigger(context.Background(), "my-trigger", map[string]any{"foo": "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/triggers/my-trigger/fire" {
		t.Errorf("expected path /triggers/my-trigger/fire, got %s", gotPath)
	}
	if resp.InstanceID != "inst-1" {
		t.Errorf("expected instance_id inst-1, got %s", resp.InstanceID)
	}
}

func TestCreateTrigger(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/triggers" {
			t.Errorf("expected path /triggers, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TriggerDef{Slug: "my-trigger", TriggerType: "webhook"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreateTrigger(context.Background(), map[string]any{"slug": "my-trigger"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Slug != "my-trigger" {
		t.Errorf("expected Slug my-trigger, got %s", resp.Slug)
	}
}

func TestListTriggers(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/triggers" {
			t.Errorf("expected path /triggers, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TriggerDef{
			{Slug: "trigger-a"},
			{Slug: "trigger-b"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListTriggers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(result))
	}
}

func TestGetTrigger(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/triggers/my-slug" {
			t.Errorf("expected path /triggers/my-slug, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TriggerDef{Slug: "my-slug", TriggerType: "webhook"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetTrigger(context.Background(), "my-slug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Slug != "my-slug" {
		t.Errorf("expected Slug my-slug, got %s", resp.Slug)
	}
}

func TestDeleteTrigger(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/triggers/my-slug" {
			t.Errorf("expected path /triggers/my-slug, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeleteTrigger(context.Background(), "my-slug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Plugins
// ---------------------------------------------------------------------------

func TestCreatePlugin(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/plugins" {
			t.Errorf("expected path /plugins, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PluginDef{Name: "my-plugin", PluginType: "http"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreatePlugin(context.Background(), map[string]any{"name": "my-plugin"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "my-plugin" {
		t.Errorf("expected Name my-plugin, got %s", resp.Name)
	}
}

func TestListPlugins(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/plugins" {
			t.Errorf("expected path /plugins, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]PluginDef{
			{Name: "plugin-a"},
			{Name: "plugin-b"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListPlugins(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(result))
	}
}

func TestGetPlugin(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/plugins/my-plugin" {
			t.Errorf("expected path /plugins/my-plugin, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PluginDef{Name: "my-plugin", PluginType: "http"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetPlugin(context.Background(), "my-plugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "my-plugin" {
		t.Errorf("expected Name my-plugin, got %s", resp.Name)
	}
}

func TestUpdatePlugin(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/plugins/my-plugin" {
			t.Errorf("expected path /plugins/my-plugin, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(PluginDef{Name: "my-plugin", Description: "updated"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.UpdatePlugin(context.Background(), "my-plugin", map[string]any{"description": "updated"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Description != "updated" {
		t.Errorf("expected Description updated, got %s", resp.Description)
	}
}

func TestDeletePlugin(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/plugins/my-plugin" {
			t.Errorf("expected path /plugins/my-plugin, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeletePlugin(context.Background(), "my-plugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

func TestCreateSession(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/sessions" {
			t.Errorf("expected path /sessions, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Session{ID: "sess-1", State: "active"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreateSession(context.Background(), map[string]any{"session_key": "key-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "sess-1" {
		t.Errorf("expected ID sess-1, got %s", resp.ID)
	}
}

func TestGetSession(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/sessions/sess-1" {
			t.Errorf("expected path /sessions/sess-1, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Session{ID: "sess-1", State: "active"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "sess-1" {
		t.Errorf("expected ID sess-1, got %s", resp.ID)
	}
}

func TestGetSessionByKey(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/sessions/by-key/tenant-1/my-key" {
			t.Errorf("expected path /sessions/by-key/tenant-1/my-key, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Session{ID: "sess-2", SessionKey: "my-key"})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetSessionByKey(context.Background(), "tenant-1", "my-key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SessionKey != "my-key" {
		t.Errorf("expected SessionKey my-key, got %s", resp.SessionKey)
	}
}

func TestUpdateSessionData(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/sessions/sess-1/data" {
			t.Errorf("expected path /sessions/sess-1/data, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	_, err := c.UpdateSessionData(context.Background(), "sess-1", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateSessionState(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/sessions/sess-1/state" {
			t.Errorf("expected path /sessions/sess-1/state, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	_, err := c.UpdateSessionState(context.Background(), "sess-1", map[string]string{"state": "closed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListSessionInstances(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/sessions/sess-1/instances" {
			t.Errorf("expected path /sessions/sess-1/instances, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]TaskInstance{
			{ID: "inst-1"},
			{ID: "inst-2"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListSessionInstances(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 instances, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Workers
// ---------------------------------------------------------------------------

func TestPollTasks(t *testing.T) {
	var gotBody map[string]any
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/workers/tasks/poll" {
			t.Errorf("expected path /workers/tasks/poll, got %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]WorkerTask{
			{ID: "task-1", HandlerName: "my-handler"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.PollTasks(context.Background(), "my-handler", "worker-1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "task-1" {
		t.Errorf("expected ID task-1, got %s", result[0].ID)
	}
	if gotBody["handler_name"] != "my-handler" {
		t.Errorf("expected handler_name my-handler, got %v", gotBody["handler_name"])
	}
	if gotBody["worker_id"] != "worker-1" {
		t.Errorf("expected worker_id worker-1, got %v", gotBody["worker_id"])
	}
}

func TestCompleteTask(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/workers/tasks/task-1/complete" {
			t.Errorf("expected path /workers/tasks/task-1/complete, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.CompleteTask(context.Background(), "task-1", "worker-1", map[string]any{"result": "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFailTask(t *testing.T) {
	var gotBody map[string]any
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/workers/tasks/task-1/fail" {
			t.Errorf("expected path /workers/tasks/task-1/fail, got %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.FailTask(context.Background(), "task-1", "worker-1", "something broke", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["message"] != "something broke" {
		t.Errorf("expected message 'something broke', got %v", gotBody["message"])
	}
	if gotBody["retryable"] != true {
		t.Errorf("expected retryable true, got %v", gotBody["retryable"])
	}
}

func TestHeartbeatTask(t *testing.T) {
	var gotBody map[string]any
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/workers/tasks/task-1/heartbeat" {
			t.Errorf("expected path /workers/tasks/task-1/heartbeat, got %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.HeartbeatTask(context.Background(), "task-1", "worker-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["worker_id"] != "worker-1" {
		t.Errorf("expected worker_id worker-1, got %v", gotBody["worker_id"])
	}
}

// ---------------------------------------------------------------------------
// Cluster
// ---------------------------------------------------------------------------

func TestListClusterNodes(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/cluster/nodes" {
			t.Errorf("expected path /cluster/nodes, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]ClusterNode{
			{ID: "node-1", Address: "10.0.0.1:8080", State: "active"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListClusterNodes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 node, got %d", len(result))
	}
	if result[0].ID != "node-1" {
		t.Errorf("expected ID node-1, got %s", result[0].ID)
	}
}

func TestDrainNode(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/cluster/nodes/node-1/drain" {
			t.Errorf("expected path /cluster/nodes/node-1/drain, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DrainNode(context.Background(), "node-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Circuit Breakers
// ---------------------------------------------------------------------------

func TestListCircuitBreakers(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/circuit-breakers" {
			t.Errorf("expected path /circuit-breakers, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]CircuitBreakerState{
			{Handler: "my-handler", State: "closed", FailureCount: 0},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListCircuitBreakers(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 circuit breaker, got %d", len(result))
	}
	if result[0].Handler != "my-handler" {
		t.Errorf("expected Handler my-handler, got %s", result[0].Handler)
	}
}

func TestGetCircuitBreaker(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/circuit-breakers/my-handler" {
			t.Errorf("expected path /circuit-breakers/my-handler, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CircuitBreakerState{Handler: "my-handler", State: "open", FailureCount: 5})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetCircuitBreaker(context.Background(), "my-handler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Handler != "my-handler" {
		t.Errorf("expected Handler my-handler, got %s", resp.Handler)
	}
	if resp.State != "open" {
		t.Errorf("expected State open, got %s", resp.State)
	}
	if resp.FailureCount != 5 {
		t.Errorf("expected FailureCount 5, got %d", resp.FailureCount)
	}
}

func TestResetCircuitBreaker(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/circuit-breakers/my-handler/reset" {
			t.Errorf("expected path /circuit-breakers/my-handler/reset, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.ResetCircuitBreaker(context.Background(), "my-handler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sequences (additional)
// ---------------------------------------------------------------------------

func TestListSequences(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/sequences", 200, []SequenceDefinition{
		{ID: "seq-1", Name: "alpha"},
		{ID: "seq-2", Name: "beta"},
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListSequences(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 sequences, got %d", len(result))
	}
	if result[0].ID != "seq-1" {
		t.Errorf("expected ID seq-1, got %s", result[0].ID)
	}
}

func TestDeleteSequence(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/sequences/seq-del" {
			t.Errorf("expected path /sequences/seq-del, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeleteSequence(context.Background(), "seq-del")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMigrateInstance(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "POST", "/sequences/migrate-instance", 200, TaskInstance{
		ID:    "inst-migrated",
		State: "pending",
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.MigrateInstance(context.Background(), map[string]any{
		"instance_id":     "inst-1",
		"new_sequence_id": "seq-2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "inst-migrated" {
		t.Errorf("expected ID inst-migrated, got %s", resp.ID)
	}
	if resp.State != "pending" {
		t.Errorf("expected State pending, got %s", resp.State)
	}
}

// ---------------------------------------------------------------------------
// Instances (additional)
// ---------------------------------------------------------------------------

func TestInjectBlocks(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/instances/inst-1/inject-blocks" {
			t.Errorf("expected path /instances/inst-1/inject-blocks, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.InjectBlocks(context.Background(), "inst-1", map[string]any{
		"blocks": []any{map[string]any{"type": "step", "handler": "foo"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

func TestListApprovals(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/approvals", 200, []TaskInstance{
		{ID: "inst-approval-1", State: "waiting_for_approval"},
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListApprovals(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(result))
	}
	if result[0].ID != "inst-approval-1" {
		t.Errorf("expected ID inst-approval-1, got %s", result[0].ID)
	}
	if result[0].State != "waiting_for_approval" {
		t.Errorf("expected State waiting_for_approval, got %s", result[0].State)
	}
}

// ---------------------------------------------------------------------------
// Workers (additional)
// ---------------------------------------------------------------------------

func TestListWorkerTasks(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/workers/tasks", 200, []WorkerTask{
		{ID: "task-1", HandlerName: "handler-a", State: "pending"},
		{ID: "task-2", HandlerName: "handler-b", State: "claimed"},
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListWorkerTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result))
	}
	if result[0].ID != "task-1" {
		t.Errorf("expected ID task-1, got %s", result[0].ID)
	}
}

func TestGetWorkerTaskStats(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/workers/tasks/stats", 200, map[string]any{
		"pending":   float64(10),
		"claimed":   float64(3),
		"completed": float64(42),
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.GetWorkerTaskStats(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["pending"] != float64(10) {
		t.Errorf("expected pending 10, got %v", result["pending"])
	}
	if result["completed"] != float64(42) {
		t.Errorf("expected completed 42, got %v", result["completed"])
	}
}

func TestPollTasksFromQueue(t *testing.T) {
	var gotBody map[string]any
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/workers/tasks/poll/my-queue" {
			t.Errorf("expected path /workers/tasks/poll/my-queue, got %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]WorkerTask{
			{ID: "task-q1", HandlerName: "handler-q"},
		})
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.PollTasksFromQueue(context.Background(), "my-queue", "worker-1", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result))
	}
	if result[0].ID != "task-q1" {
		t.Errorf("expected ID task-q1, got %s", result[0].ID)
	}
	if gotBody["worker_id"] != "worker-1" {
		t.Errorf("expected worker_id worker-1, got %v", gotBody["worker_id"])
	}
	if gotBody["limit"] != float64(5) {
		t.Errorf("expected limit 5, got %v", gotBody["limit"])
	}
}

// ---------------------------------------------------------------------------
// Resource Pools
// ---------------------------------------------------------------------------

func TestListPools(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/pools", 200, []ResourcePool{
		{ID: "pool-1", Name: "gpu-pool", MaxSize: 10},
		{ID: "pool-2", Name: "cpu-pool", MaxSize: 50},
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListPools(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(result))
	}
	if result[0].Name != "gpu-pool" {
		t.Errorf("expected Name gpu-pool, got %s", result[0].Name)
	}
}

func TestCreatePool(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "POST", "/pools", 200, ResourcePool{
		ID:      "pool-new",
		Name:    "new-pool",
		MaxSize: 20,
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreatePool(context.Background(), map[string]any{
		"name":     "new-pool",
		"max_size": 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "pool-new" {
		t.Errorf("expected ID pool-new, got %s", resp.ID)
	}
	if resp.MaxSize != 20 {
		t.Errorf("expected MaxSize 20, got %d", resp.MaxSize)
	}
}

func TestGetPool(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/pools/pool-42", 200, ResourcePool{
		ID:   "pool-42",
		Name: "my-pool",
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetPool(context.Background(), "pool-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "pool-42" {
		t.Errorf("expected ID pool-42, got %s", resp.ID)
	}
	if resp.Name != "my-pool" {
		t.Errorf("expected Name my-pool, got %s", resp.Name)
	}
}

func TestDeletePool(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/pools/pool-del" {
			t.Errorf("expected path /pools/pool-del, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeletePool(context.Background(), "pool-del")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListPoolResources(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/pools/pool-1/resources", 200, []PoolResource{
		{ID: "res-1", PoolID: "pool-1", ResourceKey: "gpu-0", State: "available"},
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListPoolResources(context.Background(), "pool-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result))
	}
	if result[0].ResourceKey != "gpu-0" {
		t.Errorf("expected ResourceKey gpu-0, got %s", result[0].ResourceKey)
	}
}

func TestCreatePoolResource(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "POST", "/pools/pool-1/resources", 200, PoolResource{
		ID:          "res-new",
		PoolID:      "pool-1",
		ResourceKey: "gpu-1",
		State:       "available",
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreatePoolResource(context.Background(), "pool-1", map[string]any{
		"resource_key": "gpu-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "res-new" {
		t.Errorf("expected ID res-new, got %s", resp.ID)
	}
	if resp.ResourceKey != "gpu-1" {
		t.Errorf("expected ResourceKey gpu-1, got %s", resp.ResourceKey)
	}
}

func TestUpdatePoolResource(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "PUT", "/pools/pool-1/resources/res-1", 200, PoolResource{
		ID:          "res-1",
		PoolID:      "pool-1",
		ResourceKey: "gpu-0",
		State:       "locked",
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.UpdatePoolResource(context.Background(), "pool-1", "res-1", map[string]any{
		"state": "locked",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.State != "locked" {
		t.Errorf("expected State locked, got %s", resp.State)
	}
}

func TestDeletePoolResource(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/pools/pool-1/resources/res-del" {
			t.Errorf("expected path /pools/pool-1/resources/res-del, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeletePoolResource(context.Background(), "pool-1", "res-del")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

func TestListCredentials(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/credentials", 200, []Credential{
		{ID: "cred-1", Name: "api-key", CredentialType: "api_key"},
		{ID: "cred-2", Name: "oauth-token", CredentialType: "oauth"},
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListCredentials(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 credentials, got %d", len(result))
	}
	if result[0].Name != "api-key" {
		t.Errorf("expected Name api-key, got %s", result[0].Name)
	}
}

func TestCreateCredential(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "POST", "/credentials", 200, Credential{
		ID:             "cred-new",
		Name:           "my-secret",
		CredentialType: "api_key",
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.CreateCredential(context.Background(), map[string]any{
		"name":            "my-secret",
		"credential_type": "api_key",
		"value":           "super-secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "cred-new" {
		t.Errorf("expected ID cred-new, got %s", resp.ID)
	}
	if resp.Name != "my-secret" {
		t.Errorf("expected Name my-secret, got %s", resp.Name)
	}
}

func TestGetCredential(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/credentials/cred-42", 200, Credential{
		ID:             "cred-42",
		Name:           "my-cred",
		CredentialType: "oauth",
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetCredential(context.Background(), "cred-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ID != "cred-42" {
		t.Errorf("expected ID cred-42, got %s", resp.ID)
	}
	if resp.CredentialType != "oauth" {
		t.Errorf("expected CredentialType oauth, got %s", resp.CredentialType)
	}
}

func TestDeleteCredential(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/credentials/cred-del" {
			t.Errorf("expected path /credentials/cred-del, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeleteCredential(context.Background(), "cred-del")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateCredential(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "PATCH", "/credentials/cred-1", 200, Credential{
		ID:             "cred-1",
		Name:           "renamed-cred",
		CredentialType: "api_key",
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.UpdateCredential(context.Background(), "cred-1", map[string]any{
		"name": "renamed-cred",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Name != "renamed-cred" {
		t.Errorf("expected Name renamed-cred, got %s", resp.Name)
	}
}

// ---------------------------------------------------------------------------
// Circuit Breakers (per-tenant)
// ---------------------------------------------------------------------------

func TestListTenantCircuitBreakers(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/tenants/t1/circuit-breakers", 200, []CircuitBreakerState{
		{Handler: "handler-a", State: "closed", FailureCount: 0},
		{Handler: "handler-b", State: "open", FailureCount: 3},
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	result, err := c.ListTenantCircuitBreakers(context.Background(), "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 circuit breakers, got %d", len(result))
	}
	if result[0].Handler != "handler-a" {
		t.Errorf("expected Handler handler-a, got %s", result[0].Handler)
	}
	if result[1].State != "open" {
		t.Errorf("expected State open, got %s", result[1].State)
	}
}

func TestGetTenantCircuitBreaker(t *testing.T) {
	srv := newTestServer(t, jsonHandler(t, "GET", "/tenants/t1/circuit-breakers/my-handler", 200, CircuitBreakerState{
		Handler:      "my-handler",
		State:        "half-open",
		FailureCount: 2,
	}))

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	resp, err := c.GetTenantCircuitBreaker(context.Background(), "t1", "my-handler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Handler != "my-handler" {
		t.Errorf("expected Handler my-handler, got %s", resp.Handler)
	}
	if resp.State != "half-open" {
		t.Errorf("expected State half-open, got %s", resp.State)
	}
	if resp.FailureCount != 2 {
		t.Errorf("expected FailureCount 2, got %d", resp.FailureCount)
	}
}

func TestResetTenantCircuitBreaker(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/tenants/t1/circuit-breakers/my-handler/reset" {
			t.Errorf("expected path /tenants/t1/circuit-breakers/my-handler/reset, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.ResetTenantCircuitBreaker(context.Background(), "t1", "my-handler")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

