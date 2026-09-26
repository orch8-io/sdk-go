package orch8

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContinuityClientPaths(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && (r.URL.Path == "/runtimes" || r.URL.Path == "/continuity/executions/e 1/effects") {
			_ = json.NewEncoder(w).Encode([]JSONObject{{"id": "x"}})
			return
		}
		_ = json.NewEncoder(w).Encode(JSONObject{"id": "x"})
	}))
	defer srv.Close()
	cc := NewClient(ClientConfig{BaseURL: srv.URL}).Continuity()
	ctx := context.Background()
	if _, err := cc.CreateExecution(ctx, JSONObject{"tenant_id": "t"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.GetExecution(ctx, "e 1", "t"); err != nil {
		t.Fatal(err)
	}
	if list, err := cc.ListRuntimes(ctx, "t"); err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	if list, err := cc.ListEffects(ctx, "e 1", "t"); err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	if _, err := cc.AcceptHandoff(ctx, "h1", JSONObject{}); err != nil {
		t.Fatal(err)
	}
	if _, err := cc.VerifyProvenance(ctx, "e1", "t", "abc"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"POST /continuity/executions",
		"GET /continuity/executions/e%201?tenant_id=t",
		"GET /runtimes?tenant_id=t",
		"GET /continuity/executions/e%201/effects?tenant_id=t",
		"POST /continuity/handoffs/h1/accept",
		"GET /continuity/executions/e1/provenance/verify?expected_head=abc&tenant_id=t",
	}
	for i, w := range want {
		if seen[i] != w {
			t.Errorf("call %d: got %q want %q", i, seen[i], w)
		}
	}
}
