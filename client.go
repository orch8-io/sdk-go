// Package orch8 provides a Go client for the Orch8 workflow engine REST API.
package orch8

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientConfig holds configuration for the Orch8 API client.
type ClientConfig struct {
	BaseURL  string
	TenantID string
	Headers  map[string]string
	// HTTPClient allows overriding the default [*http.Client].
	// If nil, a client with a 30-second timeout is used.
	HTTPClient *http.Client
	// GetHeaders resolves short-lived auth headers before every attempt.
	GetHeaders func(context.Context) (map[string]string, error)
	// RetryMaxAttempts is the total number of safe-request attempts. Zero uses 3.
	// Set it to 1 to disable retries.
	RetryMaxAttempts int
	// RetryBaseDelay is the initial exponential-backoff delay. Zero uses 250ms.
	RetryBaseDelay time.Duration
	// OnRetry is called before a retry; attempt is the one-based next attempt.
	OnRetry func(error, int)
	// OnRequest and OnResponse observe attempts without changing request behavior.
	OnRequest  func(RequestEvent)
	OnResponse func(ResponseEvent)
}

// Client is an HTTP client for the Orch8 engine REST API.
type Client struct {
	baseURL          string
	tenantID         string
	headers          map[string]string
	http             *http.Client
	getHeaders       func(context.Context) (map[string]string, error)
	retryMaxAttempts int
	retryBaseDelay   time.Duration
	onRetry          func(error, int)
	onRequest        func(RequestEvent)
	onResponse       func(ResponseEvent)
}

func pathSegment(value string) string {
	return url.PathEscape(value)
}

// NewClient creates a new Orch8 API client.
func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	retryMaxAttempts := cfg.RetryMaxAttempts
	if retryMaxAttempts == 0 {
		retryMaxAttempts = 3
	}
	retryBaseDelay := cfg.RetryBaseDelay
	if retryBaseDelay == 0 {
		retryBaseDelay = 250 * time.Millisecond
	}
	return &Client{
		baseURL:          strings.TrimRight(cfg.BaseURL, "/"),
		tenantID:         cfg.TenantID,
		headers:          cfg.Headers,
		http:             httpClient,
		getHeaders:       cfg.GetHeaders,
		retryMaxAttempts: retryMaxAttempts,
		retryBaseDelay:   retryBaseDelay,
		onRetry:          cfg.OnRetry,
		onRequest:        cfg.OnRequest,
		onResponse:       cfg.OnResponse,
	}
}

// do performs an HTTP request and decodes the response.
func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyData []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyData = data
	}

	maxAttempts := 1
	if method == http.MethodGet || method == http.MethodHead {
		maxAttempts = c.retryMaxAttempts
		if maxAttempts < 1 {
			maxAttempts = 1
		}
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		event := RequestEvent{Method: method, Path: path, Attempt: attempt, MaxAttempts: maxAttempts}
		startedAt := time.Now()
		c.observeRequest(event)
		status, err := c.doOnce(ctx, method, path, bodyData, result)
		c.observeResponse(ResponseEvent{
			RequestEvent: event,
			DurationMs:   float64(time.Since(startedAt).Microseconds()) / 1000,
			Status:       status,
			Err:          err,
		})
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt >= maxAttempts || !isRetryableError(err) {
			return err
		}
		if c.onRetry != nil {
			c.onRetry(err, attempt+1)
		}
		delay := c.retryBaseDelay * time.Duration(1<<(attempt-1))
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return lastErr
}

func (c *Client) observeRequest(event RequestEvent) {
	defer func() { _ = recover() }()
	if c.onRequest != nil {
		c.onRequest(event)
	}
}

func (c *Client) observeResponse(event ResponseEvent) {
	defer func() { _ = recover() }()
	if c.onResponse != nil {
		c.onResponse(event)
	}
}

type transportError struct{ err error }

func (e *transportError) Error() string { return fmt.Sprintf("execute request: %v", e.err) }
func (e *transportError) Unwrap() error { return e.err }

func isRetryableError(err error) bool {
	if _, ok := err.(*transportError); ok {
		return true
	}
	if apiErr, ok := err.(*Orch8Error); ok {
		return apiErr.Status == 408 || apiErr.Status == 425 || apiErr.Status == 429 || apiErr.Status >= 500
	}
	return false
}

func (c *Client) doOnce(ctx context.Context, method, path string, bodyData []byte, result any) (int, error) {
	var bodyReader io.Reader
	if bodyData != nil {
		bodyReader = bytes.NewReader(bodyData)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.tenantID != "" {
		req.Header.Set("X-Tenant-Id", c.tenantID)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if c.getHeaders != nil {
		dynamicHeaders, err := c.getHeaders(ctx)
		if err != nil {
			return 0, fmt.Errorf("resolve request headers: %w", err)
		}
		for k, v := range dynamicHeaders {
			req.Header.Set(k, v)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, &transportError{err: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return resp.StatusCode, &Orch8Error{
			Status: resp.StatusCode,
			Body:   string(respBody),
			Path:   path,
		}
	}

	if resp.StatusCode == 204 || result == nil {
		return resp.StatusCode, nil
	}

	// Engine returns 200 with an empty body for several handlers
	// (update_state, update_context, etc.). Treat that as a successful
	// no-content response instead of surfacing "unexpected end of JSON input".
	if len(respBody) == 0 {
		return resp.StatusCode, nil
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return resp.StatusCode, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp.StatusCode, nil
}

// RequestPage calls a list endpoint while preserving pagination metadata.
func RequestPage[T any](ctx context.Context, client *Client, path string, filter map[string]string) (*Page[T], error) {
	if len(filter) > 0 {
		params := url.Values{}
		for key, value := range filter {
			params.Set(key, value)
		}
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		path += separator + params.Encode()
	}
	var raw json.RawMessage
	if err := client.Request(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) > 0 && raw[0] == '[' {
		var items []T
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("unmarshal page items: %w", err)
		}
		return &Page[T]{Items: items}, nil
	}
	var page Page[T]
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("unmarshal page: %w", err)
	}
	return &page, nil
}

// Request calls an engine endpoint not yet covered by a convenience method.
// Path must be relative to the configured engine origin. This provides forward
// compatibility for newly introduced and experimental API routes.
func (c *Client) Request(ctx context.Context, method, path string, body any, result any) error {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return fmt.Errorf("path must start with exactly one '/' character")
	}
	return c.do(ctx, method, path, body, result)
}

// ---------------------------------------------------------------------------
// Sequences
// ---------------------------------------------------------------------------

// CreateSequence registers a new sequence definition.
func (c *Client) CreateSequence(ctx context.Context, body any) (*CreateSequenceResponse, error) {
	prepared, err := c.prepareTenantNamespace(body)
	if err != nil {
		return nil, fmt.Errorf("create sequence: %w", err)
	}
	if prepared["id"] == nil || prepared["id"] == "" {
		id, err := newUUID()
		if err != nil {
			return nil, err
		}
		prepared["id"] = id
	}
	setDefault(prepared, "version", 1)
	setDefault(prepared, "deprecated", false)
	setDefault(prepared, "status", "production")
	setDefault(prepared, "created_at", time.Now().UTC().Format(time.RFC3339Nano))

	var out CreateSequenceResponse
	if err := c.do(ctx, http.MethodPost, "/sequences", prepared, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func setDefault(values map[string]any, key string, value any) {
	if current, exists := values[key]; !exists || current == nil || current == "" {
		values[key] = value
	}
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate sequence id: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[:4], value[4:6], value[6:8], value[8:10], value[10:]), nil
}

func decodeList[T any](raw json.RawMessage, resource string) ([]T, error) {
	var out []T
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, fmt.Errorf("unmarshal %s list: %w", resource, err)
		}
		return out, nil
	}
	var page struct {
		Items []T `json:"items"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("unmarshal %s page: %w", resource, err)
	}
	return page.Items, nil
}

func (c *Client) prepareTenantNamespace(body any) (map[string]any, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	var prepared map[string]any
	if err := json.Unmarshal(data, &prepared); err != nil {
		return nil, fmt.Errorf("normalize request body: %w", err)
	}
	tenantID, _ := prepared["tenant_id"].(string)
	if tenantID == "" {
		tenantID = c.tenantID
	}
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	prepared["tenant_id"] = tenantID
	setDefault(prepared, "namespace", "default")
	return prepared, nil
}

// GetSequence retrieves a sequence definition by ID.
func (c *Client) GetSequence(ctx context.Context, id string) (*SequenceDefinition, error) {
	var out SequenceDefinition
	if err := c.do(ctx, http.MethodGet, "/sequences/"+pathSegment(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSequenceByName retrieves a sequence by tenant, namespace, name, and optional version.
func (c *Client) GetSequenceByName(ctx context.Context, tenantID, namespace, name string, version *int) (*SequenceDefinition, error) {
	params := url.Values{
		"tenant_id": {tenantID},
		"namespace": {namespace},
		"name":      {name},
	}
	if version != nil {
		params.Set("version", fmt.Sprintf("%d", *version))
	}
	var out SequenceDefinition
	if err := c.do(ctx, http.MethodGet, "/sequences/by-name?"+params.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeprecateSequence marks a sequence as deprecated.
func (c *Client) DeprecateSequence(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/sequences/"+pathSegment(id)+"/deprecate", nil, nil)
}

// ListSequenceVersions lists all versions of a sequence by tenant, namespace, and name.
func (c *Client) ListSequenceVersions(ctx context.Context, tenantID, namespace, name string) ([]SequenceDefinition, error) {
	params := url.Values{
		"tenant_id": {tenantID},
		"namespace": {namespace},
		"name":      {name},
	}
	var out []SequenceDefinition
	if err := c.do(ctx, http.MethodGet, "/sequences/versions?"+params.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListSequences lists sequence definitions with optional filters.
func (c *Client) ListSequences(ctx context.Context, filter map[string]string) ([]SequenceDefinition, error) {
	path := "/sequences"
	if len(filter) > 0 {
		params := url.Values{}
		for k, v := range filter {
			params.Set(k, v)
		}
		path += "?" + params.Encode()
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	return decodeList[SequenceDefinition](raw, "sequence")
}

// DeleteSequence deletes a sequence definition by ID.
func (c *Client) DeleteSequence(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sequences/"+pathSegment(id), nil, nil)
}

// MigrateInstance migrates an instance to a different sequence version.
func (c *Client) MigrateInstance(ctx context.Context, body any) (*TaskInstance, error) {
	var out TaskInstance
	if err := c.do(ctx, http.MethodPost, "/sequences/migrate-instance", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Instances
// ---------------------------------------------------------------------------

// CreateInstance creates a new task instance.
func (c *Client) CreateInstance(ctx context.Context, body any) (*TaskInstance, error) {
	prepared, err := c.prepareTenantNamespace(body)
	if err != nil {
		return nil, fmt.Errorf("create instance: %w", err)
	}
	var out TaskInstance
	if err := c.do(ctx, http.MethodPost, "/instances", prepared, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BatchCreateInstances creates multiple task instances in one call.
func (c *Client) BatchCreateInstances(ctx context.Context, body any) (*BatchCreateResponse, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal instance batch: %w", err)
	}
	var request struct {
		Instances []json.RawMessage `json:"instances"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return nil, fmt.Errorf("normalize instance batch: %w", err)
	}
	instances := make([]map[string]any, 0, len(request.Instances))
	for _, raw := range request.Instances {
		prepared, err := c.prepareTenantNamespace(raw)
		if err != nil {
			return nil, fmt.Errorf("create instance batch: %w", err)
		}
		instances = append(instances, prepared)
	}
	var out BatchCreateResponse
	if err := c.do(ctx, http.MethodPost, "/instances/batch", map[string]any{"instances": instances}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetInstance retrieves a task instance by ID.
func (c *Client) GetInstance(ctx context.Context, id string) (*TaskInstance, error) {
	var out TaskInstance
	if err := c.do(ctx, http.MethodGet, "/instances/"+pathSegment(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListInstances lists task instances with optional filters.
func (c *Client) ListInstances(ctx context.Context, filter map[string]string) ([]TaskInstance, error) {
	path := "/instances"
	if len(filter) > 0 {
		params := url.Values{}
		for k, v := range filter {
			params.Set(k, v)
		}
		path += "?" + params.Encode()
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, err
	}
	return decodeList[TaskInstance](raw, "instance")
}

// UpdateInstanceState updates the state of an instance. The engine returns
// 200 with an empty body — call GetInstance if the updated record is needed.
func (c *Client) UpdateInstanceState(ctx context.Context, id string, body any) error {
	return c.do(ctx, http.MethodPatch, "/instances/"+pathSegment(id)+"/state", body, nil)
}

// UpdateInstanceContext updates the context of an instance. The engine
// returns 200 with an empty body.
func (c *Client) UpdateInstanceContext(ctx context.Context, id string, body any) error {
	return c.do(ctx, http.MethodPatch, "/instances/"+pathSegment(id)+"/context", body, nil)
}

// SendSignal sends a signal to an instance and returns the generated
// signal ID so callers can correlate delivery events.
func (c *Client) SendSignal(ctx context.Context, id string, body any) (*SignalResponse, error) {
	var out SignalResponse
	if err := c.do(ctx, http.MethodPost, "/instances/"+pathSegment(id)+"/signals", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOutputs retrieves step outputs for an instance.
func (c *Client) GetOutputs(ctx context.Context, id string) ([]StepOutput, error) {
	var out []StepOutput
	if err := c.do(ctx, http.MethodGet, "/instances/"+pathSegment(id)+"/outputs", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetExecutionTree retrieves the execution tree for an instance.
func (c *Client) GetExecutionTree(ctx context.Context, id string) ([]ExecutionNode, error) {
	var out []ExecutionNode
	if err := c.do(ctx, http.MethodGet, "/instances/"+pathSegment(id)+"/tree", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RetryInstance retries a failed instance.
func (c *Client) RetryInstance(ctx context.Context, id string) (*TaskInstance, error) {
	var out TaskInstance
	if err := c.do(ctx, http.MethodPost, "/instances/"+pathSegment(id)+"/retry", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCheckpoints lists checkpoints for an instance.
func (c *Client) ListCheckpoints(ctx context.Context, instanceID string) ([]Checkpoint, error) {
	var out []Checkpoint
	if err := c.do(ctx, http.MethodGet, "/instances/"+pathSegment(instanceID)+"/checkpoints", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveCheckpoint saves a checkpoint for an instance.
func (c *Client) SaveCheckpoint(ctx context.Context, instanceID string, body any) (*Checkpoint, error) {
	var out Checkpoint
	if err := c.do(ctx, http.MethodPost, "/instances/"+pathSegment(instanceID)+"/checkpoints", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLatestCheckpoint retrieves the latest checkpoint for an instance.
func (c *Client) GetLatestCheckpoint(ctx context.Context, instanceID string) (*Checkpoint, error) {
	var out Checkpoint
	if err := c.do(ctx, http.MethodGet, "/instances/"+pathSegment(instanceID)+"/checkpoints/latest", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PruneCheckpoints prunes old checkpoints, optionally keeping the last N.
// The engine contract is `{"keep": <u32>}` — see
// orch8-api::instances::PruneCheckpointsRequest. Parameter name kept as
// `keepLast` in Go for clarity at the call site.
func (c *Client) PruneCheckpoints(ctx context.Context, instanceID string, keepLast *int) error {
	var body any
	if keepLast != nil {
		body = map[string]int{"keep": *keepLast}
	}
	return c.do(ctx, http.MethodPost, "/instances/"+pathSegment(instanceID)+"/checkpoints/prune", body, nil)
}

// InjectBlocks injects blocks into a running instance.
func (c *Client) InjectBlocks(ctx context.Context, id string, body any) error {
	return c.do(ctx, http.MethodPost, "/instances/"+pathSegment(id)+"/inject-blocks", body, nil)
}

// ListAuditLog retrieves the audit log for an instance.
func (c *Client) ListAuditLog(ctx context.Context, instanceID string) ([]AuditEntry, error) {
	var out []AuditEntry
	if err := c.do(ctx, http.MethodGet, "/instances/"+pathSegment(instanceID)+"/audit", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// BulkUpdateState updates the state of multiple instances matching a filter.
func (c *Client) BulkUpdateState(ctx context.Context, filter map[string]any, newState string) (*BulkResponse, error) {
	body := map[string]any{
		"filter":    filter,
		"new_state": newState,
	}
	var out BulkResponse
	if err := c.do(ctx, http.MethodPatch, "/instances/bulk/state", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BulkReschedule reschedules multiple instances matching a filter.
func (c *Client) BulkReschedule(ctx context.Context, filter map[string]any, offsetSecs int) (*BulkResponse, error) {
	body := map[string]any{
		"filter":      filter,
		"offset_secs": offsetSecs,
	}
	var out BulkResponse
	if err := c.do(ctx, http.MethodPatch, "/instances/bulk/reschedule", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListDLQ lists instances in the dead-letter queue.
func (c *Client) ListDLQ(ctx context.Context, filter map[string]string) ([]TaskInstance, error) {
	path := "/instances/dlq"
	if len(filter) > 0 {
		params := url.Values{}
		for k, v := range filter {
			params.Set(k, v)
		}
		path += "?" + params.Encode()
	}
	var out []TaskInstance
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Cron
// ---------------------------------------------------------------------------

// CreateCron creates a new cron schedule.
func (c *Client) CreateCron(ctx context.Context, body any) (*CronSchedule, error) {
	var out CronSchedule
	if err := c.do(ctx, http.MethodPost, "/cron", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCron lists cron schedules, optionally filtered by tenant ID.
func (c *Client) ListCron(ctx context.Context, tenantID string) ([]CronSchedule, error) {
	path := "/cron"
	if tenantID != "" {
		path += "?" + url.Values{"tenant_id": {tenantID}}.Encode()
	}
	var out []CronSchedule
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCron retrieves a cron schedule by ID.
func (c *Client) GetCron(ctx context.Context, id string) (*CronSchedule, error) {
	var out CronSchedule
	if err := c.do(ctx, http.MethodGet, "/cron/"+pathSegment(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateCron updates an existing cron schedule.
func (c *Client) UpdateCron(ctx context.Context, id string, body any) (*CronSchedule, error) {
	var out CronSchedule
	if err := c.do(ctx, http.MethodPut, "/cron/"+pathSegment(id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCron deletes a cron schedule.
func (c *Client) DeleteCron(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/cron/"+pathSegment(id), nil, nil)
}

// ---------------------------------------------------------------------------
// Triggers
// ---------------------------------------------------------------------------

// CreateTrigger creates a new trigger definition.
func (c *Client) CreateTrigger(ctx context.Context, body any) (*TriggerDef, error) {
	var out TriggerDef
	if err := c.do(ctx, http.MethodPost, "/triggers", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListTriggers lists triggers, optionally filtered by tenant ID.
func (c *Client) ListTriggers(ctx context.Context, tenantID string) ([]TriggerDef, error) {
	path := "/triggers"
	if tenantID != "" {
		path += "?" + url.Values{"tenant_id": {tenantID}}.Encode()
	}
	var out []TriggerDef
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetTrigger retrieves a trigger by slug.
func (c *Client) GetTrigger(ctx context.Context, slug string) (*TriggerDef, error) {
	var out TriggerDef
	if err := c.do(ctx, http.MethodGet, "/triggers/"+pathSegment(slug), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTrigger deletes a trigger by slug.
func (c *Client) DeleteTrigger(ctx context.Context, slug string) error {
	return c.do(ctx, http.MethodDelete, "/triggers/"+pathSegment(slug), nil, nil)
}

// FireTrigger fires a trigger by slug with optional data payload.
func (c *Client) FireTrigger(ctx context.Context, slug string, data any) (*FireTriggerResponse, error) {
	var out FireTriggerResponse
	if err := c.do(ctx, http.MethodPost, "/triggers/"+pathSegment(slug)+"/fire", data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Plugins
// ---------------------------------------------------------------------------

// CreatePlugin registers a new plugin.
func (c *Client) CreatePlugin(ctx context.Context, body any) (*PluginDef, error) {
	var out PluginDef
	if err := c.do(ctx, http.MethodPost, "/plugins", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPlugins lists plugins, optionally filtered by tenant ID.
func (c *Client) ListPlugins(ctx context.Context, tenantID string) ([]PluginDef, error) {
	path := "/plugins"
	if tenantID != "" {
		path += "?" + url.Values{"tenant_id": {tenantID}}.Encode()
	}
	var out []PluginDef
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPlugin retrieves a plugin by name.
func (c *Client) GetPlugin(ctx context.Context, name string) (*PluginDef, error) {
	var out PluginDef
	if err := c.do(ctx, http.MethodGet, "/plugins/"+pathSegment(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePlugin updates an existing plugin.
func (c *Client) UpdatePlugin(ctx context.Context, name string, update any) (*PluginDef, error) {
	var out PluginDef
	if err := c.do(ctx, http.MethodPatch, "/plugins/"+pathSegment(name), update, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePlugin deletes a plugin by name.
func (c *Client) DeletePlugin(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/plugins/"+pathSegment(name), nil, nil)
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// CreateSession creates a new session.
func (c *Client) CreateSession(ctx context.Context, body any) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodPost, "/sessions", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSession retrieves a session by ID.
func (c *Client) GetSession(ctx context.Context, id string) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodGet, "/sessions/"+pathSegment(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSessionByKey retrieves a session by tenant ID and key.
func (c *Client) GetSessionByKey(ctx context.Context, tenantID, key string) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodGet, "/sessions/by-key/"+pathSegment(tenantID)+"/"+key, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSessionData updates the data of a session.
func (c *Client) UpdateSessionData(ctx context.Context, id string, body any) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+pathSegment(id)+"/data", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSessionState updates the state of a session.
func (c *Client) UpdateSessionState(ctx context.Context, id string, body any) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+pathSegment(id)+"/state", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSessionInstances lists task instances associated with a session.
func (c *Client) ListSessionInstances(ctx context.Context, id string) ([]TaskInstance, error) {
	var out []TaskInstance
	if err := c.do(ctx, http.MethodGet, "/sessions/"+pathSegment(id)+"/instances", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Workers
// ---------------------------------------------------------------------------

// PollTasks polls for available worker tasks.
func (c *Client) PollTasks(ctx context.Context, handlerName, workerID string, limit int) ([]WorkerTask, error) {
	body := map[string]any{
		"handler_name": handlerName,
		"worker_id":    workerID,
		"limit":        limit,
	}
	var out []WorkerTask
	if err := c.do(ctx, http.MethodPost, "/workers/tasks/poll", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CompleteTask marks a worker task as completed.
func (c *Client) CompleteTask(ctx context.Context, taskID string, workerID string, output any) error {
	body := map[string]any{
		"worker_id": workerID,
		"output":    output,
	}
	return c.do(ctx, http.MethodPost, "/workers/tasks/"+pathSegment(taskID)+"/complete", body, nil)
}

// FailTask marks a worker task as failed.
func (c *Client) FailTask(ctx context.Context, taskID, workerID, message string, retryable bool) error {
	body := map[string]any{
		"worker_id": workerID,
		"message":   message,
		"retryable": retryable,
	}
	return c.do(ctx, http.MethodPost, "/workers/tasks/"+pathSegment(taskID)+"/fail", body, nil)
}

// HeartbeatTask sends a heartbeat for an in-flight worker task.
func (c *Client) HeartbeatTask(ctx context.Context, taskID, workerID string) error {
	_, err := c.HeartbeatTaskWithCheckpoint(ctx, taskID, workerID, nil, nil)
	return err
}

// HeartbeatTaskWithCheckpoint heartbeats a task and optionally advances its
// resumable checkpoint using optimistic checkpoint sequencing.
func (c *Client) HeartbeatTaskWithCheckpoint(ctx context.Context, taskID, workerID string, checkpoint any, checkpointSeq *uint64) (*HeartbeatResponse, error) {
	body := map[string]any{
		"worker_id": workerID,
	}
	if checkpoint != nil {
		if checkpointSeq == nil {
			return nil, fmt.Errorf("checkpointSeq is required with checkpoint")
		}
		body["checkpoint"] = checkpoint
		body["checkpoint_seq"] = *checkpointSeq
	}
	var out HeartbeatResponse
	if err := c.do(ctx, http.MethodPost, "/workers/tasks/"+pathSegment(taskID)+"/heartbeat", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWorkerTasks lists worker tasks with optional filters.
func (c *Client) ListWorkerTasks(ctx context.Context, filter map[string]string) ([]WorkerTask, error) {
	path := "/workers/tasks"
	if len(filter) > 0 {
		params := url.Values{}
		for k, v := range filter {
			params.Set(k, v)
		}
		path += "?" + params.Encode()
	}
	var out []WorkerTask
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkerTaskStats retrieves aggregate statistics for worker tasks.
func (c *Client) GetWorkerTaskStats(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.do(ctx, http.MethodGet, "/workers/tasks/stats", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PollTasksFromQueue polls for available worker tasks from a specific queue.
func (c *Client) PollTasksFromQueue(ctx context.Context, queue, handlerName, workerID string, limit int) ([]WorkerTask, error) {
	body := map[string]any{
		"queue_name":   queue,
		"handler_name": handlerName,
		"worker_id":    workerID,
		"limit":        limit,
	}
	var out []WorkerTask
	if err := c.do(ctx, http.MethodPost, "/workers/tasks/poll/queue", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

// ListApprovals lists instances awaiting approval with optional filters.
func (c *Client) ListApprovals(ctx context.Context, filter map[string]string) (*ApprovalsResponse, error) {
	path := "/approvals"
	if len(filter) > 0 {
		params := url.Values{}
		for k, v := range filter {
			params.Set(k, v)
		}
		path += "?" + params.Encode()
	}
	var out ApprovalsResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Cluster
// ---------------------------------------------------------------------------

// ListClusterNodes lists all cluster nodes.
func (c *Client) ListClusterNodes(ctx context.Context) ([]ClusterNode, error) {
	var out []ClusterNode
	if err := c.do(ctx, http.MethodGet, "/cluster/nodes", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DrainNode initiates draining of a cluster node.
func (c *Client) DrainNode(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/cluster/nodes/"+pathSegment(id)+"/drain", nil, nil)
}

// ---------------------------------------------------------------------------
// Circuit Breakers
// ---------------------------------------------------------------------------

// ListCircuitBreakers lists all circuit breakers.
func (c *Client) ListCircuitBreakers(ctx context.Context) ([]CircuitBreakerState, error) {
	var out []CircuitBreakerState
	if err := c.do(ctx, http.MethodGet, "/circuit-breakers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetCircuitBreaker retrieves a circuit breaker by handler name.
func (c *Client) GetCircuitBreaker(ctx context.Context, handler string) (*CircuitBreakerState, error) {
	var out CircuitBreakerState
	if err := c.do(ctx, http.MethodGet, "/circuit-breakers/"+pathSegment(handler), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetCircuitBreaker resets a circuit breaker by handler name.
func (c *Client) ResetCircuitBreaker(ctx context.Context, handler string) error {
	return c.do(ctx, http.MethodPost, "/circuit-breakers/"+pathSegment(handler)+"/reset", nil, nil)
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

// Health checks the readiness of the Orch8 engine.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var out HealthResponse
	if err := c.do(ctx, http.MethodGet, "/health/ready", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Resource Pools
// ---------------------------------------------------------------------------

// ListPools lists resource pools, optionally filtered by tenant ID.
func (c *Client) ListPools(ctx context.Context, tenantID string) ([]ResourcePool, error) {
	path := "/pools"
	if tenantID != "" {
		path += "?" + url.Values{"tenant_id": {tenantID}}.Encode()
	}
	var out []ResourcePool
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreatePool creates a new resource pool.
func (c *Client) CreatePool(ctx context.Context, body any) (*ResourcePool, error) {
	var out ResourcePool
	if err := c.do(ctx, http.MethodPost, "/pools", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPool retrieves a resource pool by ID.
func (c *Client) GetPool(ctx context.Context, id string) (*ResourcePool, error) {
	var out ResourcePool
	if err := c.do(ctx, http.MethodGet, "/pools/"+pathSegment(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePool deletes a resource pool by ID.
func (c *Client) DeletePool(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/pools/"+pathSegment(id), nil, nil)
}

// ListPoolResources lists resources within a pool.
func (c *Client) ListPoolResources(ctx context.Context, poolID string) ([]PoolResource, error) {
	var out []PoolResource
	if err := c.do(ctx, http.MethodGet, "/pools/"+pathSegment(poolID)+"/resources", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreatePoolResource creates a new resource within a pool.
func (c *Client) CreatePoolResource(ctx context.Context, poolID string, body any) (*PoolResource, error) {
	var out PoolResource
	if err := c.do(ctx, http.MethodPost, "/pools/"+pathSegment(poolID)+"/resources", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePoolResource updates an existing resource within a pool.
func (c *Client) UpdatePoolResource(ctx context.Context, poolID, resourceID string, body any) (*PoolResource, error) {
	var out PoolResource
	if err := c.do(ctx, http.MethodPut, "/pools/"+pathSegment(poolID)+"/resources/"+pathSegment(resourceID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePoolResource deletes a resource from a pool.
func (c *Client) DeletePoolResource(ctx context.Context, poolID, resourceID string) error {
	return c.do(ctx, http.MethodDelete, "/pools/"+pathSegment(poolID)+"/resources/"+pathSegment(resourceID), nil, nil)
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

// ListCredentials lists credentials, optionally filtered by tenant ID.
func (c *Client) ListCredentials(ctx context.Context, tenantID string) ([]Credential, error) {
	path := "/credentials"
	if tenantID != "" {
		path += "?" + url.Values{"tenant_id": {tenantID}}.Encode()
	}
	var out []Credential
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateCredential creates a new credential.
func (c *Client) CreateCredential(ctx context.Context, body any) (*Credential, error) {
	var out Credential
	if err := c.do(ctx, http.MethodPost, "/credentials", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetCredential retrieves a credential by ID.
func (c *Client) GetCredential(ctx context.Context, id string) (*Credential, error) {
	var out Credential
	if err := c.do(ctx, http.MethodGet, "/credentials/"+pathSegment(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCredential deletes a credential by ID.
func (c *Client) DeleteCredential(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/credentials/"+pathSegment(id), nil, nil)
}

// UpdateCredential partially updates a credential by ID.
func (c *Client) UpdateCredential(ctx context.Context, id string, body any) (*Credential, error) {
	var out Credential
	if err := c.do(ctx, http.MethodPatch, "/credentials/"+pathSegment(id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Circuit Breakers (per-tenant)
// ---------------------------------------------------------------------------

// ListTenantCircuitBreakers lists circuit breakers for a specific tenant.
func (c *Client) ListTenantCircuitBreakers(ctx context.Context, tenantID string) ([]CircuitBreakerState, error) {
	var out []CircuitBreakerState
	if err := c.do(ctx, http.MethodGet, "/tenants/"+pathSegment(tenantID)+"/circuit-breakers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetTenantCircuitBreaker retrieves a circuit breaker for a specific tenant and handler.
func (c *Client) GetTenantCircuitBreaker(ctx context.Context, tenantID, handler string) (*CircuitBreakerState, error) {
	var out CircuitBreakerState
	if err := c.do(ctx, http.MethodGet, "/tenants/"+pathSegment(tenantID)+"/circuit-breakers/"+pathSegment(handler), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetTenantCircuitBreaker resets a circuit breaker for a specific tenant and handler.
func (c *Client) ResetTenantCircuitBreaker(ctx context.Context, tenantID, handler string) error {
	return c.do(ctx, http.MethodPost, "/tenants/"+pathSegment(tenantID)+"/circuit-breakers/"+pathSegment(handler)+"/reset", nil, nil)
}

// ---------------------------------------------------------------------------
// Mobile Sync
// ---------------------------------------------------------------------------

// MobileSync synchronizes mobile device state with the engine.
func (c *Client) MobileSync(ctx context.Context, body *SyncRequest) (*SyncResponse, error) {
	var out SyncResponse
	if err := c.do(ctx, http.MethodPost, "/mobile/sync", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RegisterMobileDevice registers a new mobile device.
func (c *Client) RegisterMobileDevice(ctx context.Context, body *RegisterDeviceRequest) error {
	return c.do(ctx, http.MethodPost, "/mobile/devices/register", body, nil)
}

// ListMobileDevices lists all registered mobile devices.
func (c *Client) ListMobileDevices(ctx context.Context) (*MobileDevicesResponse, error) {
	var out MobileDevicesResponse
	if err := c.do(ctx, http.MethodGet, "/mobile/devices", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListMobileApprovals lists approvals accessible from mobile devices.
func (c *Client) ListMobileApprovals(ctx context.Context) (*MobileApprovalsResponse, error) {
	var out MobileApprovalsResponse
	if err := c.do(ctx, http.MethodGet, "/mobile/approvals", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResolveMobileApproval resolves a mobile approval by ID.
func (c *Client) ResolveMobileApproval(ctx context.Context, id string, body *ResolveApprovalRequest) error {
	return c.do(ctx, http.MethodPost, "/mobile/approvals/"+pathSegment(id)+"/resolve", body, nil)
}

// ListMobileStatus lists the mobile status of instances.
func (c *Client) ListMobileStatus(ctx context.Context) (*MobileStatusResponse, error) {
	var out MobileStatusResponse
	if err := c.do(ctx, http.MethodGet, "/mobile/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateMobileCommand creates a new command for a mobile device.
func (c *Client) CreateMobileCommand(ctx context.Context, body *CreateCommandRequest) error {
	return c.do(ctx, http.MethodPost, "/mobile/commands", body, nil)
}

// ---------------------------------------------------------------------------
// Telemetry
// ---------------------------------------------------------------------------

// IngestTelemetry ingests a batch of telemetry events.
func (c *Client) IngestTelemetry(ctx context.Context, body *IngestTelemetryRequest) (*IngestResponse, error) {
	var out IngestResponse
	if err := c.do(ctx, http.MethodPost, "/telemetry/mobile", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// IngestTelemetryError ingests a single telemetry error.
func (c *Client) IngestTelemetryError(ctx context.Context, body *IngestErrorRequest) (*IngestResponse, error) {
	var out IngestResponse
	if err := c.do(ctx, http.MethodPost, "/telemetry/mobile/errors", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TelemetryDashboard queries the telemetry dashboard.
func (c *Client) TelemetryDashboard(ctx context.Context, queryType DashboardQueryType, tenantID, startTime, endTime string) (*DashboardResponse, error) {
	params := url.Values{"query_type": {string(queryType)}}
	if tenantID != "" {
		params.Set("tenant_id", tenantID)
	}
	if startTime != "" {
		params.Set("start_time", startTime)
	}
	if endTime != "" {
		params.Set("end_time", endTime)
	}
	var out DashboardResponse
	if err := c.do(ctx, http.MethodGet, "/telemetry/mobile/dashboard?"+params.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Rollback Policies
// ---------------------------------------------------------------------------

// CreateRollbackPolicy creates a new rollback policy.
func (c *Client) CreateRollbackPolicy(ctx context.Context, body *CreatePolicyRequest) (*RollbackPolicy, error) {
	var out RollbackPolicy
	if err := c.do(ctx, http.MethodPost, "/rollback-policies", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRollbackPolicies lists rollback policies for a tenant.
func (c *Client) ListRollbackPolicies(ctx context.Context, tenantID string) ([]RollbackPolicy, error) {
	path := "/rollback-policies"
	if tenantID != "" {
		path += "?" + url.Values{"tenant_id": {tenantID}}.Encode()
	}
	var out []RollbackPolicy
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetRollbackPolicy retrieves a rollback policy by name.
func (c *Client) GetRollbackPolicy(ctx context.Context, name string) (*RollbackPolicy, error) {
	var out RollbackPolicy
	if err := c.do(ctx, http.MethodGet, "/rollback-policies/"+pathSegment(name), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRollbackPolicy deletes a rollback policy by name.
func (c *Client) DeleteRollbackPolicy(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/rollback-policies/"+pathSegment(name), nil, nil)
}

// ---------------------------------------------------------------------------
// SSE Streaming
// ---------------------------------------------------------------------------

// StreamInstance opens an SSE stream for an instance and returns channels for
// events and errors. The caller should read from both channels until the error
// channel is closed. Closing the provided context cancels the stream.
func (c *Client) StreamInstance(ctx context.Context, instanceID string, pollMs int) (<-chan map[string]any, <-chan error) {
	eventCh := make(chan map[string]any)
	errCh := make(chan error, 1)
	events, errors := c.StreamInstanceEvents(ctx, instanceID, InstanceStreamOptions{PollMs: pollMs})

	go func() {
		defer close(eventCh)
		defer close(errCh)
		defer func() {
			if recovered := recover(); recovered != nil {
				select {
				case errCh <- fmt.Errorf("stream bridge panic: %v", recovered):
				default:
				}
			}
		}()
		for events != nil || errors != nil {
			select {
			case event, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				select {
				case eventCh <- event.Data:
				case <-ctx.Done():
					return
				}
			case err, ok := <-errors:
				if !ok {
					errors = nil
					continue
				}
				errCh <- err
			}
		}
	}()

	return eventCh, errCh
}

// StreamInstanceEvents streams SSE envelopes and exposes IDs for resumption.
func (c *Client) StreamInstanceEvents(ctx context.Context, instanceID string, options InstanceStreamOptions) (<-chan SSEEvent, <-chan error) {
	eventCh := make(chan SSEEvent)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)
		defer func() {
			if recovered := recover(); recovered != nil {
				select {
				case errCh <- fmt.Errorf("stream panic: %v", recovered):
				default:
				}
			}
		}()

		params := url.Values{}
		if options.PollMs > 0 {
			pollMs := max(100, min(options.PollMs, 5000))
			params.Set("poll_ms", fmt.Sprintf("%d", pollMs))
		}
		path := "/instances/" + pathSegment(instanceID) + "/stream"
		if len(params) > 0 {
			path += "?" + params.Encode()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			errCh <- fmt.Errorf("create request: %w", err)
			return
		}

		req.Header.Set("Accept", "text/event-stream")
		if options.LastEventID != "" {
			req.Header.Set("Last-Event-ID", options.LastEventID)
		}
		if c.tenantID != "" {
			req.Header.Set("X-Tenant-Id", c.tenantID)
		}
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		if c.getHeaders != nil {
			dynamicHeaders, err := c.getHeaders(ctx)
			if err != nil {
				errCh <- fmt.Errorf("resolve request headers: %w", err)
				return
			}
			for k, v := range dynamicHeaders {
				req.Header.Set(k, v)
			}
		}

		streamHTTP := *c.http
		streamHTTP.Timeout = 0
		resp, err := streamHTTP.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("execute request: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				errCh <- fmt.Errorf("read error response: %w", readErr)
				return
			}
			errCh <- &Orch8Error{
				Status: resp.StatusCode,
				Body:   string(body),
				Path:   path,
			}
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 1024*1024) // 1MB max line size for large SSE payloads
		var eventID, eventType string
		dataLines := []string{}
		emit := func() bool {
			if len(dataLines) == 0 {
				return true
			}
			raw := strings.Join(dataLines, "\n")
			dataLines = nil
			if raw == "" || raw == "[DONE]" {
				return true
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(raw), &data); err != nil {
				errCh <- fmt.Errorf("unmarshal SSE event: %w", err)
				return false
			}
			event := SSEEvent{ID: eventID, Event: eventType, Data: data}
			eventID, eventType = "", ""
			select {
			case eventCh <- event:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "id:"):
				eventID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			case strings.HasPrefix(line, "event:"):
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case strings.TrimSpace(line) == "":
				if !emit() {
					return
				}
			}
		}
		if !emit() {
			return
		}

		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("read SSE stream: %w", err)
		}
	}()

	return eventCh, errCh
}
