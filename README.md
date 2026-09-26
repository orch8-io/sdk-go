# Orch8 Go SDK

Go client for the [Orch8](https://orch8.io) workflow engine REST API.

## Installation

```bash
go get github.com/orch8-io/sdk-go
```

Requires Go 1.21+.

The client targets the Orch8 engine 0.7 contract: the worker lease protocol
(`claim_epoch` on every acknowledgement), resumable worker checkpoints, the
0.7 sequence fields (when guards, filtered retries, output schemas, local-time
delays, bounded loop history, sagas/compensation), and continuity basics. New
or experimental engine routes are immediately available through
`Client.Request`. `Client.Jobs` targets the jobs API that ships with the engine
after 0.7; it is not part of the generated route contract yet.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	orch8 "github.com/orch8-io/sdk-go"
)

func main() {
	client := orch8.NewClient(orch8.ClientConfig{
		BaseURL:  "https://api.orch8.io",
		TenantID: "my-tenant",
	})

	ctx := context.Background()
	seq, err := client.CreateSequence(ctx, map[string]any{
		"name":      "my-sequence",
		"namespace": "default",
		"blocks":    []any{},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created sequence:", seq.ID)
}
```

## Code-first workflow DSL

```go
type Charge struct { CustomerID string `json:"customer_id"`; Cents int `json:"cents"` }

builder := orch8.Workflow("checkout")
orch8.TypedStep(builder, "charge", "charge", Charge{CustomerID: "cus_123", Cents: 2500})
definition := builder.Parallel("notify",
    func(branch *orch8.WorkflowBuilder) { branch.Step("email", "send-email", map[string]any{"template": "receipt"}) },
    func(branch *orch8.WorkflowBuilder) { branch.Step("audit", "write-audit", nil) },
).Build()
if err := builder.Err(); err != nil { log.Fatal(err) }
```

The builder covers all eleven block types. `TypedStep` retains the concrete
handler parameter type while the encoded definition remains ordinary JSON.
Router conditions use an ordered `[]orch8.Route`, so first-match priority is
stable across runs.

Use `StepWith` for the typed step options of the 0.7 contract:

```go
builder := orch8.Workflow("billing").
	InputSchema(map[string]any{"type": "object", "required": []string{"customer_id"}}).
	StepWith("charge", "charge", params, orch8.StepOptions{
		When:         `data.plan == "pro"`,
		Retry:        &orch8.RetryPolicy{MaxAttempts: 5, InitialBackoff: 1000, MaxBackoff: 60000,
			RetryIf: "error.retryable", NonRetryableCodes: []string{"card_declined"}},
		OutputSchema: map[string]any{"type": "object", "required": []string{"charge_id"}},
		Delay:        &orch8.DelaySpec{FireAtLocal: "2026-03-09T09:00:00", Timezone: "America/New_York"},
		Compensation: &orch8.StepCompensation{Handler: "refund"},
	}).
	LoopWith("poll", "!data.settled", body, orch8.LoopOptions{MaxIterations: 500, RetainIterations: orch8.Ptr(10)}).
	RaceWith("fastest", orch8.RaceFirstToSucceed, branchA, branchB).
	OnFailure(func(b *orch8.WorkflowBuilder) { b.Step("alert", "notify_oncall", nil) })
if err := builder.Err(); err != nil { log.Fatal(err) }
```

`Saga` takes one action (and at most one compensation) per step; invalid shapes
are reported by `Err()` rather than panicking.

```go
var engineInfo map[string]any
err := client.Request(ctx, http.MethodGet, "/info", nil, &engineInfo)
```

Safe requests retry transient `408`, `425`, `429`, and `5xx` responses up to
three times. The default HTTP client has a 30-second timeout. Use `GetHeaders`
to resolve short-lived credentials before every attempt, or set
`RetryMaxAttempts: 1` to disable retries.

```go
client := orch8.NewClient(orch8.ClientConfig{
	BaseURL: "https://api.orch8.io",
	GetHeaders: func(ctx context.Context) (map[string]string, error) {
		return map[string]string{"Authorization": "Bearer " + getToken(ctx)}, nil
	},
})
```

Observe attempts and preserve list pagination metadata without bypassing the
configured transport:

```go
client := orch8.NewClient(orch8.ClientConfig{
	BaseURL: "https://api.orch8.io",
	OnResponse: func(event orch8.ResponseEvent) { recordLatency(event.DurationMs) },
})
page, err := orch8.RequestPage[orch8.TaskInstance](ctx, client, "/instances", map[string]string{"limit": "50"})
```

`StreamInstanceEvents` exposes SSE IDs and accepts `LastEventID` for resuming a
later connection. Resource IDs are encoded as individual path segments.
`APIRoutes` and `APIVersion` are generated from the engine OpenAPI contract,
and `worker.Stats()` returns the portable worker-capacity snapshot.

## Worker

Run a polling worker that claims and executes tasks:

```go
worker := orch8.NewWorker(orch8.WorkerConfig{
	Client:   client,
	WorkerID: "worker-1",
	Handlers: map[string]orch8.HandlerFunc{
		"send-email": func(ctx context.Context, task orch8.WorkerTask) (any, error) {
			// process task...
			return map[string]any{"sent": true}, nil
		},
	},
	MaxConcurrent: 10,
})

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Blocks until ctx is cancelled or worker.Stop() is called.
worker.Start(ctx)
```

The worker echoes each task's `claim_epoch` on heartbeat, completion, and
failure, respects the server's `poll_after_ms`, and heartbeats no less often
than the advertised interval or half the lease. Completion callbacks run only
after the engine accepts the acknowledgement; a rejected (409) or ambiguous
acknowledgement is left for lease recovery. Custom loops should use
`PollTaskBatch` and the `*Lease` methods (`CompleteTaskLease`,
`FailTaskLease`, `HeartbeatTaskLease`); the older `CompleteTask`/`FailTask`/
`HeartbeatTask` send `claim_epoch: 0` and are deprecated.

## Jobs

One-off background jobs without authoring a sequence:

```go
job, err := client.Jobs.Enqueue(ctx, orch8.EnqueueJobRequest{
	Handler:        "send-email",
	Payload:        map[string]any{"to": "ada@example.com"},
	Retry:          &orch8.JobRetry{MaxAttempts: 5, InitialBackoffMs: 1000},
	DelayMs:        orch8.Ptr(int64(60_000)), // or RunAt: &t
	IdempotencyKey: "welcome-ada",
})
done, err := client.Jobs.WaitFor(ctx, job.ID, orch8.WaitOptions{Timeout: time.Minute})

it := client.Jobs.List(ctx, orch8.JobListOptions{Status: orch8.JobFailed})
for it.Next() { fmt.Println(it.Job().ID) }
if err := it.Err(); err != nil { log.Fatal(err) }
_ = client.Jobs.Cancel(ctx, job.ID)
```

## Push dispatch

A queue in `push` mode makes the engine POST a signed task envelope to your URL.
The pushed task is still pending and acknowledgements need a lease, so the
handler verifies the signature (`X-Orch8-Timestamp` + `X-Orch8-Signature`,
HMAC-SHA256 over `"{ts}.{body}"`, constant-time, 5-minute skew window), then
claims from the envelope's queue and runs what it claimed through the normal
worker path:

```go
h, err := orch8.NewPushHandler(client, handlers, orch8.PushHandlerOptions{
	Secret: os.Getenv("ORCH8_PUSH_SECRET"), // required unless AllowUnsigned: true
})
http.Handle("/orch8/push", h)
```

The engine waits up to 10 seconds per push attempt and retries on errors or
timeouts. Set `Async: true` to reply `202` and run longer handlers in the
background. `VerifyPushSignature` and `ParsePushEnvelope` are exported for
custom frameworks, and `PushProcessor` is framework-neutral.

AWS Lambda (Function URL, API Gateway v2 and REST proxy) lives in a separate
module so `aws-lambda-go` stays out of the core dependency graph:

```go
import orch8lambda "github.com/orch8-io/sdk-go/lambda"

h, err := orch8lambda.NewFunctionURLHandler(client, handlers, orch8.PushHandlerOptions{Secret: secret})
lambda.Start(h)
```

## Continuity

`client.Continuity()` wraps the basics of the portable-continuity control
plane (executions, runtimes, handoffs, grants, effects, provenance, placement)
with open JSON bodies.

## Testing with orch8test

`orch8test` is an `httptest` fake engine: worker poll/queue poll with lease
checks, heartbeat/complete/fail, instances, and jobs (retries, idempotency,
delays on a fake clock). It does not interpret sequences; enqueue tasks
directly or via jobs.

```go
eng := orch8test.NewEngine(t)
task := eng.EnqueueTask("send-email", map[string]any{"to": "a@b.c"})
w := orch8.NewWorker(orch8.WorkerConfig{Client: eng.Client(), WorkerID: "w", Handlers: handlers})
go w.Start(ctx)
rec := eng.WaitForTask(t, task.ID, 5*time.Second) // rec.State, rec.Output, rec.FailMessage

eng.Advance(time.Hour)      // release delayed jobs
eng.ExpireLease(task.ID)    // simulate reclaim; stale acks get 409
body := eng.PushEnvelope(task.ID)
headers := orch8test.SignedPushHeaders(secret, body)
```

## Error Handling

```go
seq, err := client.GetSequence(ctx, "non-existent")
if err != nil {
	var apiErr *orch8.Orch8Error
	if errors.As(err, &apiErr) {
		if apiErr.IsNotFound() {
			fmt.Println("Sequence not found")
		}
	}
}
```

## Development

```bash
go test ./... && go vet ./...
(cd lambda && go test ./... && go vet ./...)
```
