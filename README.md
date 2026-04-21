# Orch8 Go SDK

Go client for the [Orch8](https://orch8.io) workflow engine REST API.

## Installation

```bash
go get github.com/orch8-io/sdk-go
```

Requires Go 1.21+.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

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
