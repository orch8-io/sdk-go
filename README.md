# Orch8 Go SDK

Go client for the [Orch8](https://orch8.io) workflow engine REST API.

## Installation

```bash
go get github.com/orch8-io/sdk-go
```

Requires Go 1.21+.

The current client understands the Orch8 0.7-dev response contract for sequence
lifecycle metadata and resumable worker checkpoints. New or experimental
engine routes are immediately available through `Client.Request`.

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
go test ./...
```
