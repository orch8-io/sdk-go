package orch8

import (
	"encoding/json"
	"fmt"
)

// DelaySpec is a durable wait before a step runs. Durations are integer
// milliseconds. Set FireAtLocal (e.g. "2026-03-08T09:00:00") with Timezone
// (an IANA name such as "America/New_York") to target a local wall-clock time.
type DelaySpec struct {
	Duration         int64    `json:"duration"`
	BusinessDaysOnly bool     `json:"business_days_only,omitempty"`
	Jitter           int64    `json:"jitter,omitempty"`
	Holidays         []string `json:"holidays,omitempty"`
	FireAtLocal      string   `json:"fire_at_local,omitempty"`
	Timezone         string   `json:"timezone,omitempty"`
}

// RetryPolicy configures automatic retries. RetryIf is an expression that must
// evaluate truthy for a failure to be retried; NonRetryableCodes lists error
// codes that are never retried (filtered retries).
type RetryPolicy struct {
	MaxAttempts       int      `json:"max_attempts"`
	InitialBackoff    int64    `json:"initial_backoff"`
	MaxBackoff        int64    `json:"max_backoff"`
	BackoffMultiplier float64  `json:"backoff_multiplier,omitempty"`
	RetryIf           string   `json:"retry_if,omitempty"`
	NonRetryableCodes []string `json:"non_retryable_codes,omitempty"`
}

// SendWindow restricts execution to local hours/days (0=Monday .. 6=Sunday).
type SendWindow struct {
	StartHour *int  `json:"start_hour,omitempty"`
	EndHour   *int  `json:"end_hour,omitempty"`
	Days      []int `json:"days,omitempty"`
}

// HumanChoiceOption is one selectable answer for wait_for_input.
type HumanChoiceOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// HumanInputDef pauses a step for human input.
type HumanInputDef struct {
	Prompt            string              `json:"prompt,omitempty"`
	Timeout           int64               `json:"timeout,omitempty"`
	EscalationHandler string              `json:"escalation_handler,omitempty"`
	Choices           []HumanChoiceOption `json:"choices,omitempty"`
	StoreAs           string              `json:"store_as,omitempty"`
	AllowComment      bool                `json:"allow_comment,omitempty"`
}

// EscalationDef names the handler run when a step's deadline is breached.
type EscalationDef struct {
	Handler string `json:"handler"`
	Params  any    `json:"params,omitempty"`
}

// StepCompensation declares the undo action for a step (saga semantics on a
// flat step). Verification is one of "handler_result", "provider_receipt",
// or "manual".
type StepCompensation struct {
	Handler      string   `json:"handler"`
	Params       any      `json:"params,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
	Verification string   `json:"verification,omitempty"`
}

// StepOptions are the typed optional fields of a step block.
type StepOptions struct {
	Delay            *DelaySpec        `json:"delay,omitempty"`
	Retry            *RetryPolicy      `json:"retry,omitempty"`
	Timeout          int64             `json:"timeout,omitempty"`
	RateLimitKey     string            `json:"rate_limit_key,omitempty"`
	SendWindow       *SendWindow       `json:"send_window,omitempty"`
	ContextAccess    any               `json:"context_access,omitempty"`
	Cancellable      *bool             `json:"cancellable,omitempty"`
	WaitForInput     *HumanInputDef    `json:"wait_for_input,omitempty"`
	QueueName        string            `json:"queue_name,omitempty"`
	Deadline         int64             `json:"deadline,omitempty"`
	OnDeadlineBreach *EscalationDef    `json:"on_deadline_breach,omitempty"`
	FallbackHandler  string            `json:"fallback_handler,omitempty"`
	CacheKey         string            `json:"cache_key,omitempty"`
	OutputSchema     any               `json:"output_schema,omitempty"`
	When             string            `json:"when,omitempty"`
	Compensation     *StepCompensation `json:"compensation,omitempty"`
}

var validVerifications = map[string]bool{"": true, "handler_result": true, "provider_receipt": true, "manual": true}

func (o StepOptions) validate(id string) error {
	if r := o.Retry; r != nil {
		if r.MaxAttempts <= 0 {
			return fmt.Errorf("step %s: retry.max_attempts must be positive", id)
		}
		if r.InitialBackoff < 0 || r.MaxBackoff < 0 || r.BackoffMultiplier < 0 {
			return fmt.Errorf("step %s: retry backoff values must be non-negative", id)
		}
	}
	if d := o.Delay; d != nil {
		if d.Duration < 0 || d.Jitter < 0 {
			return fmt.Errorf("step %s: delay durations must be non-negative", id)
		}
		if d.FireAtLocal != "" && d.Timezone == "" {
			return fmt.Errorf("step %s: delay.fire_at_local requires delay.timezone", id)
		}
	}
	if c := o.Compensation; c != nil {
		if c.Handler == "" {
			return fmt.Errorf("step %s: compensation.handler is required", id)
		}
		if !validVerifications[c.Verification] {
			return fmt.Errorf("step %s: invalid compensation.verification %q", id, c.Verification)
		}
	}
	return nil
}

func (o StepOptions) fields() (map[string]any, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// StepWith appends a step with typed options (retry filters, when guards,
// output schemas, local-time delays, compensation, ...).
func (b *WorkflowBuilder) StepWith(id, handler string, params any, opts StepOptions) *WorkflowBuilder {
	if err := opts.validate(id); err != nil {
		if b.err == nil {
			b.err = err
		}
		return b
	}
	fields, err := opts.fields()
	if err != nil {
		if b.err == nil {
			b.err = fmt.Errorf("step %s: encode options: %w", id, err)
		}
		return b
	}
	return b.Step(id, handler, params, fields)
}

// TypedStepWith is StepWith with a concrete parameter type.
func TypedStepWith[P any](b *WorkflowBuilder, id, handler string, params P, opts StepOptions) *WorkflowBuilder {
	return b.StepWith(id, handler, params, opts)
}

// Delay appends a standalone durable wait, encoded as a noop step with a delay.
func (b *WorkflowBuilder) Delay(id string, spec DelaySpec) *WorkflowBuilder {
	return b.StepWith(id, "noop", nil, StepOptions{Delay: &spec})
}

// Race semantics.
const (
	RaceFirstToResolve = "first_to_resolve"
	RaceFirstToSucceed = "first_to_succeed"
)

// RaceWith appends a race with explicit semantics (RaceFirstToResolve or
// RaceFirstToSucceed).
func (b *WorkflowBuilder) RaceWith(id, semantics string, branches ...Branch) *WorkflowBuilder {
	block := Block{"type": "race", "id": id, "branches": b.branches(branches)}
	if semantics != "" {
		if semantics != RaceFirstToResolve && semantics != RaceFirstToSucceed {
			if b.err == nil {
				b.err = fmt.Errorf("race %s: invalid semantics %q", id, semantics)
			}
			return b
		}
		block["semantics"] = semantics
	}
	return b.Raw(block)
}

// LoopOptions are the optional loop fields. RetainIterations bounds stored
// history to the most recent N iterations' outputs.
type LoopOptions struct {
	MaxIterations    int
	BreakOn          string
	ContinueOnError  bool
	PollInterval     int64
	RetainIterations *int
}

// LoopWith appends a loop with full options, including bounded history.
func (b *WorkflowBuilder) LoopWith(id, condition string, body Branch, opts LoopOptions) *WorkflowBuilder {
	block := Block{"type": "loop", "id": id, "condition": condition, "body": b.branch(body)}
	if opts.MaxIterations > 0 {
		block["max_iterations"] = opts.MaxIterations
	}
	if opts.BreakOn != "" {
		block["break_on"] = opts.BreakOn
	}
	if opts.ContinueOnError {
		block["continue_on_error"] = true
	}
	if opts.PollInterval > 0 {
		block["poll_interval"] = opts.PollInterval
	}
	if !b.setRetain(block, id, opts.RetainIterations) {
		return b
	}
	return b.Raw(block)
}

// ForEachOptions are the optional for_each fields.
type ForEachOptions struct {
	ItemVar          string
	MaxIterations    int
	RetainIterations *int
}

// ForEachWith appends a for_each with full options, including bounded history.
func (b *WorkflowBuilder) ForEachWith(id, collection string, body Branch, opts ForEachOptions) *WorkflowBuilder {
	block := Block{"type": "for_each", "id": id, "collection": collection, "body": b.branch(body)}
	if opts.ItemVar != "" {
		block["item_var"] = opts.ItemVar
	}
	if opts.MaxIterations > 0 {
		block["max_iterations"] = opts.MaxIterations
	}
	if !b.setRetain(block, id, opts.RetainIterations) {
		return b
	}
	return b.Raw(block)
}

func (b *WorkflowBuilder) setRetain(block Block, id string, retain *int) bool {
	if retain == nil {
		return true
	}
	if *retain < 0 {
		if b.err == nil {
			b.err = fmt.Errorf("%s: retain_iterations must be non-negative", id)
		}
		return false
	}
	block["retain_iterations"] = *retain
	return true
}

// InputSchema sets the JSON Schema validated against instance input.
func (b *WorkflowBuilder) InputSchema(schema any) *WorkflowBuilder {
	b.inputSchema = schema
	return b
}

// OnFailure sets best-effort cleanup blocks run when an instance fails.
func (b *WorkflowBuilder) OnFailure(body Branch) *WorkflowBuilder {
	b.onFailure = b.branch(body)
	return b
}

// OnCancel sets cleanup blocks run when an instance is cancelled.
func (b *WorkflowBuilder) OnCancel(body Branch) *WorkflowBuilder {
	b.onCancel = b.branch(body)
	return b
}

// Ptr returns a pointer to v; handy for optional builder fields.
func Ptr[T any](v T) *T { return &v }
