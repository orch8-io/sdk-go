// Package orch8 provides a Go client for the Orch8 workflow engine REST API.
package orch8

import (
	"bytes"
	"context"
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
}

// Client is an HTTP client for the Orch8 engine REST API.
type Client struct {
	baseURL  string
	tenantID string
	headers  map[string]string
	http     *http.Client
}

// NewClient creates a new Orch8 API client.
func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		tenantID: cfg.TenantID,
		headers:  cfg.Headers,
		http:     httpClient,
	}
}

// do performs an HTTP request and decodes the response.
func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.tenantID != "" {
		req.Header.Set("X-Tenant-Id", c.tenantID)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &Orch8Error{
			Status: resp.StatusCode,
			Body:   string(respBody),
			Path:   path,
		}
	}

	if resp.StatusCode == 204 || result == nil {
		return nil
	}

	// Engine returns 200 with an empty body for several handlers
	// (update_state, update_context, etc.). Treat that as a successful
	// no-content response instead of surfacing "unexpected end of JSON input".
	if len(respBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sequences
// ---------------------------------------------------------------------------

// CreateSequence registers a new sequence definition.
func (c *Client) CreateSequence(ctx context.Context, body any) (*SequenceDefinition, error) {
	var out SequenceDefinition
	if err := c.do(ctx, http.MethodPost, "/sequences", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSequence retrieves a sequence definition by ID.
func (c *Client) GetSequence(ctx context.Context, id string) (*SequenceDefinition, error) {
	var out SequenceDefinition
	if err := c.do(ctx, http.MethodGet, "/sequences/"+id, nil, &out); err != nil {
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
	return c.do(ctx, http.MethodPost, "/sequences/"+id+"/deprecate", nil, nil)
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
	var out []SequenceDefinition
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteSequence deletes a sequence definition by ID.
func (c *Client) DeleteSequence(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/sequences/"+id, nil, nil)
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
	var out TaskInstance
	if err := c.do(ctx, http.MethodPost, "/instances", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BatchCreateInstances creates multiple task instances in one call.
func (c *Client) BatchCreateInstances(ctx context.Context, body any) (*BatchCreateResponse, error) {
	var out BatchCreateResponse
	if err := c.do(ctx, http.MethodPost, "/instances/batch", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetInstance retrieves a task instance by ID.
func (c *Client) GetInstance(ctx context.Context, id string) (*TaskInstance, error) {
	var out TaskInstance
	if err := c.do(ctx, http.MethodGet, "/instances/"+id, nil, &out); err != nil {
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
	var out []TaskInstance
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateInstanceState updates the state of an instance. The engine returns
// 200 with an empty body — call GetInstance if the updated record is needed.
func (c *Client) UpdateInstanceState(ctx context.Context, id string, body any) error {
	return c.do(ctx, http.MethodPatch, "/instances/"+id+"/state", body, nil)
}

// UpdateInstanceContext updates the context of an instance. The engine
// returns 200 with an empty body.
func (c *Client) UpdateInstanceContext(ctx context.Context, id string, body any) error {
	return c.do(ctx, http.MethodPatch, "/instances/"+id+"/context", body, nil)
}

// SendSignal sends a signal to an instance and returns the generated
// signal ID so callers can correlate delivery events.
func (c *Client) SendSignal(ctx context.Context, id string, body any) (*SignalResponse, error) {
	var out SignalResponse
	if err := c.do(ctx, http.MethodPost, "/instances/"+id+"/signals", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOutputs retrieves step outputs for an instance.
func (c *Client) GetOutputs(ctx context.Context, id string) ([]StepOutput, error) {
	var out []StepOutput
	if err := c.do(ctx, http.MethodGet, "/instances/"+id+"/outputs", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetExecutionTree retrieves the execution tree for an instance.
func (c *Client) GetExecutionTree(ctx context.Context, id string) ([]ExecutionNode, error) {
	var out []ExecutionNode
	if err := c.do(ctx, http.MethodGet, "/instances/"+id+"/tree", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RetryInstance retries a failed instance.
func (c *Client) RetryInstance(ctx context.Context, id string) (*TaskInstance, error) {
	var out TaskInstance
	if err := c.do(ctx, http.MethodPost, "/instances/"+id+"/retry", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListCheckpoints lists checkpoints for an instance.
func (c *Client) ListCheckpoints(ctx context.Context, instanceID string) ([]Checkpoint, error) {
	var out []Checkpoint
	if err := c.do(ctx, http.MethodGet, "/instances/"+instanceID+"/checkpoints", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SaveCheckpoint saves a checkpoint for an instance.
func (c *Client) SaveCheckpoint(ctx context.Context, instanceID string, body any) (*Checkpoint, error) {
	var out Checkpoint
	if err := c.do(ctx, http.MethodPost, "/instances/"+instanceID+"/checkpoints", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLatestCheckpoint retrieves the latest checkpoint for an instance.
func (c *Client) GetLatestCheckpoint(ctx context.Context, instanceID string) (*Checkpoint, error) {
	var out Checkpoint
	if err := c.do(ctx, http.MethodGet, "/instances/"+instanceID+"/checkpoints/latest", nil, &out); err != nil {
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
	return c.do(ctx, http.MethodPost, "/instances/"+instanceID+"/checkpoints/prune", body, nil)
}

// InjectBlocks injects blocks into a running instance.
func (c *Client) InjectBlocks(ctx context.Context, id string, body any) error {
	return c.do(ctx, http.MethodPost, "/instances/"+id+"/inject-blocks", body, nil)
}

// ListAuditLog retrieves the audit log for an instance.
func (c *Client) ListAuditLog(ctx context.Context, instanceID string) ([]AuditEntry, error) {
	var out []AuditEntry
	if err := c.do(ctx, http.MethodGet, "/instances/"+instanceID+"/audit", nil, &out); err != nil {
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
	if err := c.do(ctx, http.MethodGet, "/cron/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateCron updates an existing cron schedule.
func (c *Client) UpdateCron(ctx context.Context, id string, body any) (*CronSchedule, error) {
	var out CronSchedule
	if err := c.do(ctx, http.MethodPut, "/cron/"+id, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCron deletes a cron schedule.
func (c *Client) DeleteCron(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/cron/"+id, nil, nil)
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
	if err := c.do(ctx, http.MethodGet, "/triggers/"+slug, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTrigger deletes a trigger by slug.
func (c *Client) DeleteTrigger(ctx context.Context, slug string) error {
	return c.do(ctx, http.MethodDelete, "/triggers/"+slug, nil, nil)
}

// FireTrigger fires a trigger by slug with optional data payload.
func (c *Client) FireTrigger(ctx context.Context, slug string, data any) (*FireTriggerResponse, error) {
	var out FireTriggerResponse
	if err := c.do(ctx, http.MethodPost, "/triggers/"+slug+"/fire", data, &out); err != nil {
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
	if err := c.do(ctx, http.MethodGet, "/plugins/"+name, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePlugin updates an existing plugin.
func (c *Client) UpdatePlugin(ctx context.Context, name string, update any) (*PluginDef, error) {
	var out PluginDef
	if err := c.do(ctx, http.MethodPatch, "/plugins/"+name, update, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePlugin deletes a plugin by name.
func (c *Client) DeletePlugin(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/plugins/"+name, nil, nil)
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
	if err := c.do(ctx, http.MethodGet, "/sessions/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSessionByKey retrieves a session by tenant ID and key.
func (c *Client) GetSessionByKey(ctx context.Context, tenantID, key string) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodGet, "/sessions/by-key/"+tenantID+"/"+key, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSessionData updates the data of a session.
func (c *Client) UpdateSessionData(ctx context.Context, id string, body any) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+id+"/data", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSessionState updates the state of a session.
func (c *Client) UpdateSessionState(ctx context.Context, id string, body any) (*Session, error) {
	var out Session
	if err := c.do(ctx, http.MethodPatch, "/sessions/"+id+"/state", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSessionInstances lists task instances associated with a session.
func (c *Client) ListSessionInstances(ctx context.Context, id string) ([]TaskInstance, error) {
	var out []TaskInstance
	if err := c.do(ctx, http.MethodGet, "/sessions/"+id+"/instances", nil, &out); err != nil {
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
	return c.do(ctx, http.MethodPost, "/workers/tasks/"+taskID+"/complete", body, nil)
}

// FailTask marks a worker task as failed.
func (c *Client) FailTask(ctx context.Context, taskID, workerID, message string, retryable bool) error {
	body := map[string]any{
		"worker_id": workerID,
		"message":   message,
		"retryable": retryable,
	}
	return c.do(ctx, http.MethodPost, "/workers/tasks/"+taskID+"/fail", body, nil)
}

// HeartbeatTask sends a heartbeat for an in-flight worker task.
func (c *Client) HeartbeatTask(ctx context.Context, taskID, workerID string) error {
	body := map[string]any{
		"worker_id": workerID,
	}
	return c.do(ctx, http.MethodPost, "/workers/tasks/"+taskID+"/heartbeat", body, nil)
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
func (c *Client) PollTasksFromQueue(ctx context.Context, queue, workerID string, limit int) ([]WorkerTask, error) {
	body := map[string]any{
		"worker_id": workerID,
		"limit":     limit,
	}
	var out []WorkerTask
	if err := c.do(ctx, http.MethodPost, "/workers/tasks/poll/"+queue, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

// ListApprovals lists instances awaiting approval with optional filters.
func (c *Client) ListApprovals(ctx context.Context, filter map[string]string) ([]TaskInstance, error) {
	path := "/approvals"
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
	return c.do(ctx, http.MethodPost, "/cluster/nodes/"+id+"/drain", nil, nil)
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
	if err := c.do(ctx, http.MethodGet, "/circuit-breakers/"+handler, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetCircuitBreaker resets a circuit breaker by handler name.
func (c *Client) ResetCircuitBreaker(ctx context.Context, handler string) error {
	return c.do(ctx, http.MethodPost, "/circuit-breakers/"+handler+"/reset", nil, nil)
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
	if err := c.do(ctx, http.MethodGet, "/pools/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePool deletes a resource pool by ID.
func (c *Client) DeletePool(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/pools/"+id, nil, nil)
}

// ListPoolResources lists resources within a pool.
func (c *Client) ListPoolResources(ctx context.Context, poolID string) ([]PoolResource, error) {
	var out []PoolResource
	if err := c.do(ctx, http.MethodGet, "/pools/"+poolID+"/resources", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreatePoolResource creates a new resource within a pool.
func (c *Client) CreatePoolResource(ctx context.Context, poolID string, body any) (*PoolResource, error) {
	var out PoolResource
	if err := c.do(ctx, http.MethodPost, "/pools/"+poolID+"/resources", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePoolResource updates an existing resource within a pool.
func (c *Client) UpdatePoolResource(ctx context.Context, poolID, resourceID string, body any) (*PoolResource, error) {
	var out PoolResource
	if err := c.do(ctx, http.MethodPut, "/pools/"+poolID+"/resources/"+resourceID, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePoolResource deletes a resource from a pool.
func (c *Client) DeletePoolResource(ctx context.Context, poolID, resourceID string) error {
	return c.do(ctx, http.MethodDelete, "/pools/"+poolID+"/resources/"+resourceID, nil, nil)
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
	if err := c.do(ctx, http.MethodGet, "/credentials/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteCredential deletes a credential by ID.
func (c *Client) DeleteCredential(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/credentials/"+id, nil, nil)
}

// UpdateCredential partially updates a credential by ID.
func (c *Client) UpdateCredential(ctx context.Context, id string, body any) (*Credential, error) {
	var out Credential
	if err := c.do(ctx, http.MethodPatch, "/credentials/"+id, body, &out); err != nil {
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
	if err := c.do(ctx, http.MethodGet, "/tenants/"+tenantID+"/circuit-breakers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetTenantCircuitBreaker retrieves a circuit breaker for a specific tenant and handler.
func (c *Client) GetTenantCircuitBreaker(ctx context.Context, tenantID, handler string) (*CircuitBreakerState, error) {
	var out CircuitBreakerState
	if err := c.do(ctx, http.MethodGet, "/tenants/"+tenantID+"/circuit-breakers/"+handler, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResetTenantCircuitBreaker resets a circuit breaker for a specific tenant and handler.
func (c *Client) ResetTenantCircuitBreaker(ctx context.Context, tenantID, handler string) error {
	return c.do(ctx, http.MethodPost, "/tenants/"+tenantID+"/circuit-breakers/"+handler+"/reset", nil, nil)
}
