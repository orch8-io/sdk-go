package orch8

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Network edge cases
// ---------------------------------------------------------------------------

func TestClientHTTPTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL, HTTPClient: &http.Client{Timeout: 10 * time.Millisecond}})
	_, err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestClientEmpty200Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// intentionally write nothing
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.UpdateInstanceState(context.Background(), "inst-1", map[string]any{"state": "completed"})
	if err != nil {
		t.Fatalf("unexpected error for empty 200 body: %v", err)
	}
}

func TestClientHTMLErrorBody(t *testing.T) {
	htmlBody := "<html><body><h1>502 Bad Gateway</h1></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(htmlBody))
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	err := c.DeleteSequence(context.Background(), "seq-1")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*Orch8Error)
	if !ok {
		t.Fatalf("expected *Orch8Error, got %T", err)
	}
	if !apiErr.IsServerError() {
		t.Error("expected IsServerError == true")
	}
	// Error message should be truncated.
	if len(apiErr.Error()) > 600 {
		t.Error("expected truncated error message")
	}
}

func TestClientAcceptHeader(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	c.DeleteSequence(context.Background(), "seq-1")
	if gotAccept != "application/json" {
		t.Errorf("expected Accept header application/json, got %q", gotAccept)
	}
}

// ---------------------------------------------------------------------------
// Health with network errors
// ---------------------------------------------------------------------------

func TestHealthUnavailable(t *testing.T) {
	// Server that immediately closes connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Skip("server does not support hijacking")
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	_, err := c.Health(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			return
		}
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Health(ctx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
