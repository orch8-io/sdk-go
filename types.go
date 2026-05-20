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
	QueueName      string `json:"queue_name,omitempty"`
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

// SignalResponse is the 201 body returned by the engine when a signal is
// enqueued: `{"signal_id": "<uuid>"}`.
type SignalResponse struct {
	SignalID string `json:"signal_id"`
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

// ---------------------------------------------------------------------------
// Mobile Sync
// ---------------------------------------------------------------------------

// HumanChoice represents a single choice in an approval request.
type HumanChoice struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// StatusUpdatePayload represents a status update sent from a mobile device.
type StatusUpdatePayload struct {
	InstanceID     string  `json:"instance_id"`
	SequenceName   string  `json:"sequence_name"`
	State          string  `json:"state"`
	CurrentStep    *string `json:"current_step,omitempty"`
	Handler        *string `json:"handler,omitempty"`
	Timestamp      *string `json:"timestamp,omitempty"`
	ContextSummary any     `json:"context_summary,omitempty"`
	Steps          []any   `json:"steps,omitempty"`
}

// ApprovalRequestPayload represents an approval request sent from a mobile device.
type ApprovalRequestPayload struct {
	InstanceID     string        `json:"instance_id"`
	BlockID        string        `json:"block_id"`
	SequenceName   string        `json:"sequence_name"`
	Prompt         string        `json:"prompt"`
	Choices        []HumanChoice `json:"choices,omitempty"`
	StoreAs        *string       `json:"store_as,omitempty"`
	TimeoutSeconds *int          `json:"timeout_seconds,omitempty"`
	Metadata       any           `json:"metadata,omitempty"`
}

// StepDelegationPayload represents a step delegation sent from a mobile device.
type StepDelegationPayload struct {
	RequestID  string `json:"request_id"`
	InstanceID string `json:"instance_id"`
	BlockID    string `json:"block_id"`
	Handler    string `json:"handler"`
	Params     any    `json:"params,omitempty"`
}

// CommandPayload represents a command sent to a mobile device.
type CommandPayload struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// SyncRequest is the request body for the mobile sync endpoint.
type SyncRequest struct {
	DeviceID         string                   `json:"device_id"`
	StatusUpdates    []StatusUpdatePayload    `json:"status_updates,omitempty"`
	ApprovalRequests []ApprovalRequestPayload `json:"approval_requests,omitempty"`
	StepDelegations  []StepDelegationPayload  `json:"step_delegations,omitempty"`
	CommandAcks      []string                 `json:"command_acks,omitempty"`
}

// SyncResponse is the response from the mobile sync endpoint.
type SyncResponse struct {
	Commands         []CommandPayload `json:"commands"`
	SyncIntervalSecs int              `json:"sync_interval_secs"`
}

// RegisterDeviceRequest is the request body for registering a mobile device.
type RegisterDeviceRequest struct {
	DeviceID   string  `json:"device_id"`
	PushToken  *string `json:"push_token,omitempty"`
	Platform   string  `json:"platform"`
	AppVersion *string `json:"app_version,omitempty"`
}

// ResolveApprovalRequest is the request body for resolving a mobile approval.
type ResolveApprovalRequest struct {
	Output any `json:"output,omitempty"`
}

// CreateCommandRequest is the request body for creating a mobile command.
type CreateCommandRequest struct {
	DeviceID    string `json:"device_id"`
	CommandType string `json:"command_type"`
	Payload     any    `json:"payload,omitempty"`
}

// MobileDevice represents a registered mobile device.
type MobileDevice struct {
	ID         string  `json:"id"`
	DeviceID   string  `json:"device_id"`
	PushToken  *string `json:"push_token,omitempty"`
	Platform   string  `json:"platform"`
	AppVersion *string `json:"app_version,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// MobileStatus represents the status of an instance from a mobile perspective.
type MobileStatus struct {
	InstanceID  string  `json:"instance_id"`
	State       string  `json:"state"`
	CurrentStep *string `json:"current_step,omitempty"`
	UpdatedAt   string  `json:"updated_at"`
}

// MobileDevicesResponse is the response from the mobile devices list endpoint.
type MobileDevicesResponse struct {
	Items []MobileDevice `json:"items"`
	Total int            `json:"total"`
}

// MobileApprovalsResponse is the response from the mobile approvals list endpoint.
type MobileApprovalsResponse struct {
	Items []ApprovalItem `json:"items"`
	Total int            `json:"total"`
}

// MobileStatusResponse is the response from the mobile status list endpoint.
type MobileStatusResponse struct {
	Items []MobileStatus `json:"items"`
	Total int            `json:"total"`
}

// ---------------------------------------------------------------------------
// Telemetry
// ---------------------------------------------------------------------------

// DeviceContext represents device information for telemetry events.
type DeviceContext struct {
	DeviceID   string  `json:"device_id"`
	OSName     *string `json:"os_name,omitempty"`
	OSVersion  *string `json:"os_version,omitempty"`
	AppVersion *string `json:"app_version,omitempty"`
	SDKVersion *string `json:"sdk_version,omitempty"`
}

// TelemetryBatchItem represents a single telemetry event in a batch.
type TelemetryBatchItem struct {
	EventType string         `json:"event_type"`
	Payload   any            `json:"payload,omitempty"`
	Timestamp *string        `json:"timestamp,omitempty"`
	Device    *DeviceContext `json:"device,omitempty"`
}

// IngestTelemetryRequest is the request body for ingesting telemetry events.
type IngestTelemetryRequest struct {
	Events   []TelemetryBatchItem `json:"events"`
	TenantID *string              `json:"tenant_id,omitempty"`
}

// IngestErrorRequest is the request body for ingesting a telemetry error.
type IngestErrorRequest struct {
	ErrorType    string         `json:"error_type"`
	Message      string         `json:"message"`
	StackTrace   *string        `json:"stack_trace,omitempty"`
	Device       *DeviceContext `json:"device,omitempty"`
	TenantID     *string        `json:"tenant_id,omitempty"`
	InstanceID   *string        `json:"instance_id,omitempty"`
	SequenceName *string        `json:"sequence_name,omitempty"`
}

// IngestResponse is the response from telemetry ingestion endpoints.
type IngestResponse struct {
	Accepted int `json:"accepted"`
}

// DashboardQueryType represents the type of dashboard query.
type DashboardQueryType string

// Dashboard query type constants.
const (
	DashboardQuerySyncCompletedVersions DashboardQueryType = "SyncCompletedVersions"
	DashboardQueryErrorRatePerSequence  DashboardQueryType = "ErrorRatePerSequence"
	DashboardQueryTopFailingSteps       DashboardQueryType = "TopFailingSteps"
	DashboardQueryDeviceOsBreakdown     DashboardQueryType = "DeviceOsBreakdown"
)

// DashboardRow represents a single row in a dashboard response.
type DashboardRow struct {
	Dimension  string  `json:"dimension"`
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

// DashboardResponse is the response from the telemetry dashboard endpoint.
type DashboardResponse struct {
	Rows []DashboardRow `json:"rows"`
}

// ---------------------------------------------------------------------------
// Rollback Policies
// ---------------------------------------------------------------------------

// RollbackPolicy represents a rollback policy configuration.
type RollbackPolicy struct {
	ID                     int64   `json:"id"`
	TenantID               string  `json:"tenant_id"`
	SequenceName           string  `json:"sequence_name"`
	ErrorRateThreshold     float64 `json:"error_rate_threshold"`
	TimeWindowSecs         int     `json:"time_window_secs"`
	Enabled                bool    `json:"enabled"`
	CooldownSecs           int     `json:"cooldown_secs"`
	ConfirmationWindowSecs int     `json:"confirmation_window_secs"`
	WebhookURL             *string `json:"webhook_url,omitempty"`
	CreatedAt              string  `json:"created_at"`
	UpdatedAt              string  `json:"updated_at"`
}

// CreatePolicyRequest is the request body for creating a rollback policy.
type CreatePolicyRequest struct {
	TenantID               string  `json:"tenant_id"`
	SequenceName           string  `json:"sequence_name"`
	ErrorRateThreshold     float64 `json:"error_rate_threshold"`
	TimeWindowSecs         int     `json:"time_window_secs"`
	CooldownSecs           *int    `json:"cooldown_secs,omitempty"`
	ConfirmationWindowSecs *int    `json:"confirmation_window_secs,omitempty"`
	WebhookURL             *string `json:"webhook_url,omitempty"`
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

// ApprovalItem represents a single approval waiting for human input.
type ApprovalItem struct {
	InstanceID        string        `json:"instance_id"`
	TenantID          string        `json:"tenant_id"`
	Namespace         string        `json:"namespace"`
	SequenceID        string        `json:"sequence_id"`
	SequenceName      string        `json:"sequence_name"`
	BlockID           string        `json:"block_id"`
	Prompt            string        `json:"prompt"`
	Choices           []HumanChoice `json:"choices"`
	StoreAs           *string       `json:"store_as,omitempty"`
	TimeoutSeconds    *uint64       `json:"timeout_seconds,omitempty"`
	EscalationHandler *string       `json:"escalation_handler,omitempty"`
	WaitingSince      string        `json:"waiting_since"`
	Deadline          *string       `json:"deadline,omitempty"`
	Metadata          any           `json:"metadata,omitempty"`
	AllowComment      bool          `json:"allow_comment"`
}

// ApprovalsResponse is the response from the approvals list endpoint.
type ApprovalsResponse struct {
	Items []ApprovalItem `json:"items"`
	Total uint64         `json:"total"`
}

// ---------------------------------------------------------------------------
// Typed Request Payloads
// ---------------------------------------------------------------------------

// CreateInstanceRequest is the typed request body for creating an instance.
type CreateInstanceRequest struct {
	SequenceID       string `json:"sequence_id"`
	TenantID         string `json:"tenant_id,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	Priority         int    `json:"priority,omitempty"`
	Timezone         string `json:"timezone,omitempty"`
	Metadata         any    `json:"metadata,omitempty"`
	Context          any    `json:"context,omitempty"`
	ConcurrencyKey   string `json:"concurrency_key,omitempty"`
	MaxConcurrency   *int   `json:"max_concurrency,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	ParentInstanceID string `json:"parent_instance_id,omitempty"`
	NextFireAt       string `json:"next_fire_at,omitempty"`
}

// UpdateStateRequest is the typed request body for updating instance state.
type UpdateStateRequest struct {
	State       string `json:"state"`
	NextFireAt  string `json:"next_fire_at,omitempty"`
}

// UpdateContextRequest is the typed request body for updating instance context.
type UpdateContextRequest struct {
	Context any `json:"context"`
}

// SendSignalRequest is the typed request body for sending a signal.
type SendSignalRequest struct {
	SignalType string `json:"signal_type,omitempty"`
	Payload    any    `json:"payload,omitempty"`
}

// CreateCronRequest is the typed request body for creating a cron schedule.
type CreateCronRequest struct {
	TenantID   string `json:"tenant_id"`
	Namespace  string `json:"namespace"`
	SequenceID string `json:"sequence_id"`
	CronExpr   string `json:"cron_expr"`
	Timezone   string `json:"timezone,omitempty"`
	Enabled    *bool  `json:"enabled,omitempty"`
	Metadata   any    `json:"metadata,omitempty"`
}

// UpdateCronRequest is the typed request body for updating a cron schedule.
type UpdateCronRequest struct {
	CronExpr   string  `json:"cron_expr,omitempty"`
	Timezone   string  `json:"timezone,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
	Metadata   any     `json:"metadata,omitempty"`
	SequenceID *string `json:"sequence_id,omitempty"`
}

// CreateTriggerRequest is the typed request body for creating a trigger.
type CreateTriggerRequest struct {
	Slug         string `json:"slug"`
	SequenceName string `json:"sequence_name"`
	Version      *int   `json:"version,omitempty"`
	TenantID     string `json:"tenant_id"`
	Namespace    string `json:"namespace"`
	Enabled      bool   `json:"enabled,omitempty"`
	Secret       string `json:"secret,omitempty"`
	TriggerType  string `json:"trigger_type"`
	Config       any    `json:"config,omitempty"`
}

// CreateCredentialRequest is the typed request body for creating a credential.
type CreateCredentialRequest struct {
	TenantID       string         `json:"tenant_id"`
	Name           string         `json:"name"`
	CredentialType string         `json:"credential_type"`
	Value          string         `json:"value,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// UpdateCredentialRequest is the typed request body for updating a credential.
type UpdateCredentialRequest struct {
	Name           string         `json:"name,omitempty"`
	CredentialType string         `json:"credential_type,omitempty"`
	Value          string         `json:"value,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// CreateSessionRequest is the typed request body for creating a session.
type CreateSessionRequest struct {
	TenantID   string `json:"tenant_id"`
	SessionKey string `json:"session_key"`
	State      string `json:"state,omitempty"`
	Data       any    `json:"data,omitempty"`
}

// CreatePoolRequest is the typed request body for creating a resource pool.
type CreatePoolRequest struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Strategy string `json:"strategy,omitempty"`
	Config   any    `json:"config,omitempty"`
}

// AddResourceRequest is the typed request body for adding a resource to a pool.
type AddResourceRequest struct {
	ResourceKey    string `json:"resource_key"`
	Name           string `json:"name"`
	Weight         int    `json:"weight,omitempty"`
	DailyCap       int    `json:"daily_cap,omitempty"`
	WarmupStart    string `json:"warmup_start,omitempty"`
	WarmupDays     int    `json:"warmup_days,omitempty"`
	WarmupStartCap int    `json:"warmup_start_cap,omitempty"`
	Data           any    `json:"data,omitempty"`
}

// UpdateResourceRequest is the typed request body for updating a pool resource.
type UpdateResourceRequest struct {
	Name           string `json:"name,omitempty"`
	Weight         int    `json:"weight,omitempty"`
	Enabled        *bool  `json:"enabled,omitempty"`
	DailyCap       int    `json:"daily_cap,omitempty"`
	WarmupStart    string `json:"warmup_start,omitempty"`
	WarmupDays     int    `json:"warmup_days,omitempty"`
	WarmupStartCap int    `json:"warmup_start_cap,omitempty"`
	Data           any    `json:"data,omitempty"`
}

// PollRequest is the typed request body for polling tasks.
type PollRequest struct {
	HandlerName string `json:"handler_name"`
	WorkerID    string `json:"worker_id"`
	Limit       int    `json:"limit"`
}

// QueuePollRequest is the typed request body for polling tasks from a queue.
type QueuePollRequest struct {
	QueueName   string `json:"queue_name"`
	HandlerName string `json:"handler_name"`
	WorkerID    string `json:"worker_id"`
	Limit       int    `json:"limit"`
}

// CompleteRequest is the typed request body for completing a task.
type CompleteRequest struct {
	WorkerID string `json:"worker_id"`
	Output   any    `json:"output,omitempty"`
}

// FailRequest is the typed request body for failing a task.
type FailRequest struct {
	WorkerID  string `json:"worker_id"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// HeartbeatRequest is the typed request body for sending a heartbeat.
type HeartbeatRequest struct {
	WorkerID string `json:"worker_id"`
}
