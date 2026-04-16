package orch8

// SequenceDefinition represents a registered sequence blueprint.
type SequenceDefinition struct {
	ID           string `json:"id"`
	TenantID     string `json:"tenant_id"`
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Version      int    `json:"version"`
	Deprecated   bool   `json:"deprecated"`
	Blocks       []any  `json:"blocks"`
	Interceptors any    `json:"interceptors,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// TaskInstance represents a running or completed instance of a sequence.
type TaskInstance struct {
	ID               string `json:"id"`
	SequenceID       string `json:"sequence_id"`
	TenantID         string `json:"tenant_id"`
	Namespace        string `json:"namespace"`
	State            string `json:"state"`
	NextFireAt       string `json:"next_fire_at,omitempty"`
	Priority         int    `json:"priority"`
	Timezone         string `json:"timezone"`
	Metadata         any    `json:"metadata,omitempty"`
	Context          any    `json:"context,omitempty"`
	ConcurrencyKey   string `json:"concurrency_key,omitempty"`
	MaxConcurrency   *int   `json:"max_concurrency,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	ParentInstanceID string `json:"parent_instance_id,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// ExecutionNode represents a single node in the execution tree.
type ExecutionNode struct {
	ID          string `json:"id"`
	InstanceID  string `json:"instance_id"`
	BlockID     string `json:"block_id"`
	ParentID    string `json:"parent_id,omitempty"`
	BlockType   string `json:"block_type"`
	BranchIndex *int   `json:"branch_index,omitempty"`
	State       string `json:"state"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// StepOutput represents the output of a single step execution.
type StepOutput struct {
	ID         string  `json:"id"`
	InstanceID string  `json:"instance_id"`
	BlockID    string  `json:"block_id"`
	Output     any     `json:"output,omitempty"`
	OutputRef  *string `json:"output_ref,omitempty"`
	OutputSize int     `json:"output_size"`
	Attempt    int     `json:"attempt"`
	CreatedAt  string  `json:"created_at"`
}

// Checkpoint represents a saved checkpoint for an instance.
type Checkpoint struct {
	ID             string `json:"id"`
	InstanceID     string `json:"instance_id"`
	CheckpointData any    `json:"checkpoint_data,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Timestamp string `json:"timestamp"`
	Event     string `json:"event"`
	Details   any    `json:"details,omitempty"`
}

// CronSchedule represents a cron-based schedule for launching sequences.
type CronSchedule struct {
	ID              string  `json:"id"`
	TenantID        string  `json:"tenant_id"`
	Namespace       string  `json:"namespace"`
	SequenceID      string  `json:"sequence_id"`
	CronExpr        string  `json:"cron_expr"`
	Timezone        string  `json:"timezone"`
	Enabled         bool    `json:"enabled"`
	Metadata        any     `json:"metadata,omitempty"`
	LastTriggeredAt *string `json:"last_triggered_at,omitempty"`
	NextFireAt      *string `json:"next_fire_at,omitempty"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// TriggerDef represents a webhook or event trigger definition.
type TriggerDef struct {
	Slug         string `json:"slug"`
	SequenceName string `json:"sequence_name"`
	Version      *int   `json:"version,omitempty"`
	TenantID     string `json:"tenant_id"`
	Namespace    string `json:"namespace"`
	Enabled      bool   `json:"enabled"`
	Secret       string `json:"secret,omitempty"`
	TriggerType  string `json:"trigger_type"`
	Config       any    `json:"config,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// PluginDef represents a registered plugin.
type PluginDef struct {
	Name        string `json:"name"`
	PluginType  string `json:"plugin_type"`
	Source      string `json:"source"`
	TenantID    string `json:"tenant_id"`
	Enabled     bool   `json:"enabled"`
	Config      any    `json:"config,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Session represents a stateful session.
type Session struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	SessionKey string `json:"session_key"`
	State      string `json:"state"`
	Data       any    `json:"data,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// WorkerTask represents a task assigned to an external worker.
type WorkerTask struct {
	ID             string `json:"id"`
	InstanceID     string `json:"instance_id"`
	BlockID        string `json:"block_id"`
	HandlerName    string `json:"handler_name"`
	Params         any    `json:"params,omitempty"`
	Context        any    `json:"context,omitempty"`
	Attempt        int    `json:"attempt"`
	TimeoutMs      *int   `json:"timeout_ms,omitempty"`
	State          string `json:"state"`
	WorkerID       string `json:"worker_id,omitempty"`
	ClaimedAt      string `json:"claimed_at,omitempty"`
	HeartbeatAt    string `json:"heartbeat_at,omitempty"`
	CompletedAt    string `json:"completed_at,omitempty"`
	Output         any    `json:"output,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	ErrorRetryable *bool  `json:"error_retryable,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// ClusterNode represents a node in the engine cluster.
type ClusterNode struct {
	ID            string `json:"id"`
	Address       string `json:"address"`
	State         string `json:"state"`
	LastHeartbeat string `json:"last_heartbeat"`
}

// CircuitBreakerState represents the state of a circuit breaker.
type CircuitBreakerState struct {
	Handler      string `json:"handler"`
	State        string `json:"state"`
	FailureCount int    `json:"failure_count"`
	LastFailure  string `json:"last_failure,omitempty"`
}

// FireTriggerResponse is returned when a trigger is fired.
type FireTriggerResponse struct {
	InstanceID   string `json:"instance_id"`
	Trigger      string `json:"trigger"`
	SequenceName string `json:"sequence_name"`
}

// BulkResponse is returned by bulk update operations.
type BulkResponse struct {
	Updated int `json:"updated"`
}

// BatchCreateResponse is returned by batch create operations.
type BatchCreateResponse struct {
	Created int `json:"created"`
}

// HealthResponse is returned by the health endpoint.
type HealthResponse struct {
	Status string `json:"status"`
}

// ResourcePool represents a managed resource pool.
type ResourcePool struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	MaxSize     int    `json:"max_size"`
	CurrentSize int    `json:"current_size"`
	Config      any    `json:"config,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// PoolResource represents a single resource within a pool.
type PoolResource struct {
	ID          string `json:"id"`
	PoolID      string `json:"pool_id"`
	ResourceKey string `json:"resource_key"`
	State       string `json:"state"`
	Data        any    `json:"data,omitempty"`
	LockedBy    string `json:"locked_by,omitempty"`
	LockedAt    string `json:"locked_at,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Credential represents a stored credential (secret material redacted in responses).
type Credential struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	Name           string         `json:"name"`
	CredentialType string         `json:"credential_type"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}
