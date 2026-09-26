package orch8

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Push dispatch: a queue configured with mode "push" makes the engine POST a
// signed task envelope to a URL instead of waiting for a poll. The engine
// signs with the outbound-webhook scheme:
//
//	X-Orch8-Timestamp: <unix seconds>
//	X-Orch8-Signature: sha256=<hex HMAC-SHA256(secret, "<timestamp>.<body>")>
//
// The pushed task is still pending in the engine: completion and failure
// require a claimed lease (worker_id + claim_epoch), which the envelope does
// not carry. The handler here therefore treats a verified push as a wake-up,
// claims from the envelope's queue (POST /workers/tasks/poll/queue), runs the
// claimed tasks through the normal worker path, and acknowledges them under
// their lease.
const (
	PushTimestampHeader = "X-Orch8-Timestamp"
	PushSignatureHeader = "X-Orch8-Signature"
	// DefaultPushTolerance bounds clock skew / replay age for signed pushes.
	DefaultPushTolerance = 5 * time.Minute
	defaultPushMaxBody   = 10 << 20
)

// Push verification errors.
var (
	ErrPushMissingSignature = errors.New("orch8: push signature headers missing")
	ErrPushInvalidSignature = errors.New("orch8: push signature invalid")
	ErrPushStaleTimestamp   = errors.New("orch8: push timestamp outside tolerance")
)

// SignPush computes the X-Orch8-Signature header value for a body. It is the
// engine's signing scheme, exposed for tests and fake engines.
func SignPush(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifyPushSignature checks a push's timestamp and signature headers against
// the raw request body using a constant-time comparison. tolerance <= 0 uses
// DefaultPushTolerance; timestamps further than tolerance in the past or
// future are rejected to bound replay.
func VerifyPushSignature(secret, timestamp, signature string, body []byte, tolerance time.Duration) error {
	return verifyPushAt(secret, timestamp, signature, body, tolerance, time.Now())
}

func verifyPushAt(secret, timestamp, signature string, body []byte, tolerance time.Duration, now time.Time) error {
	if timestamp == "" || signature == "" {
		return ErrPushMissingSignature
	}
	if tolerance <= 0 {
		tolerance = DefaultPushTolerance
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if err != nil {
		return ErrPushInvalidSignature
	}
	age := now.Sub(time.Unix(ts, 0))
	if age > tolerance || age < -tolerance {
		return ErrPushStaleTimestamp
	}
	got, ok := strings.CutPrefix(strings.TrimSpace(signature), "sha256=")
	if !ok {
		return ErrPushInvalidSignature
	}
	gotBytes, err := hex.DecodeString(got)
	if err != nil {
		return ErrPushInvalidSignature
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	if !hmac.Equal(gotBytes, mac.Sum(nil)) {
		return ErrPushInvalidSignature
	}
	return nil
}

// PushEnvelope is the task envelope the engine POSTs to a push queue.
type PushEnvelope struct {
	TaskID      string          `json:"task_id"`
	InstanceID  string          `json:"instance_id"`
	BlockID     string          `json:"block_id"`
	HandlerName string          `json:"handler_name"`
	QueueName   string          `json:"queue_name"`
	Params      json.RawMessage `json:"params,omitempty"`
	Context     json.RawMessage `json:"context,omitempty"`
	Attempt     int             `json:"attempt"`
	TimeoutMs   *int            `json:"timeout_ms,omitempty"`
}

// ParsePushEnvelope decodes and minimally validates a push body.
func ParsePushEnvelope(body []byte) (*PushEnvelope, error) {
	var env PushEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("orch8: decode push envelope: %w", err)
	}
	if env.TaskID == "" || env.HandlerName == "" {
		return nil, errors.New("orch8: push envelope requires task_id and handler_name")
	}
	return &env, nil
}

// PushHandlerOptions configures push receivers.
type PushHandlerOptions struct {
	// Secret is the queue's dispatch secret. Required unless AllowUnsigned.
	Secret string
	// AllowUnsigned accepts pushes without signature headers. Only use this
	// behind a network boundary you trust; anyone who can reach the URL can
	// trigger claims.
	AllowUnsigned bool
	// Tolerance bounds timestamp skew; zero uses DefaultPushTolerance.
	Tolerance time.Duration
	// WorkerID identifies this receiver when claiming. Defaults to
	// "push-<hostname>-<pid>".
	WorkerID string
	// MaxClaim is how many tasks one push may claim from the queue (default 1).
	MaxClaim int
	// Async (net/http only) acknowledges a verified push with 202 and claims
	// and runs it in the background. The engine waits at most 10s per push
	// attempt and re-pushes on timeout, so use Async for handlers that may
	// exceed that. Not for Lambda, where work must finish before returning.
	Async bool
	// MaxBodyBytes caps the request body (default 10 MiB).
	MaxBodyBytes int64
	// HeartbeatInterval caps the lease heartbeat interval (default 15s; the
	// server's advertised interval / half-lease is used when shorter).
	HeartbeatInterval time.Duration
	OnTaskComplete    func(task WorkerTask, output any)
	OnTaskFail        func(task WorkerTask, err error)
	Logger            *slog.Logger
	now               func() time.Time
}

// PushResult is the outcome of processing one push.
type PushResult struct {
	// Status is the HTTP status to return: 200 processed (even if nothing was
	// left to claim), 400 bad body, 401 bad signature, 422 unknown handler or
	// queue-less envelope, 502 claim failed (the engine retries >= 400).
	Status  int      `json:"-"`
	Claimed []string `json:"claimed"`
	Error   string   `json:"error,omitempty"`
}

// PushProcessor verifies and executes pushes independent of any HTTP
// framework; NewPushHandler and the lambda adapter wrap it.
type PushProcessor struct {
	client   *Client
	handlers map[string]HandlerFunc
	opts     PushHandlerOptions
	logger   *slog.Logger
}

// NewPushProcessor validates options and builds a processor.
func NewPushProcessor(client *Client, handlers map[string]HandlerFunc, opts PushHandlerOptions) (*PushProcessor, error) {
	if client == nil {
		return nil, errors.New("orch8: push processor requires a client")
	}
	if opts.Secret == "" && !opts.AllowUnsigned {
		return nil, errors.New("orch8: push secret is empty; set AllowUnsigned to accept unsigned pushes explicitly")
	}
	if opts.WorkerID == "" {
		host, _ := os.Hostname()
		opts.WorkerID = fmt.Sprintf("push-%s-%d", host, os.Getpid())
	}
	if opts.MaxClaim <= 0 {
		opts.MaxClaim = 1
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = defaultPushMaxBody
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PushProcessor{client: client, handlers: handlers, opts: opts, logger: logger}, nil
}

// MaxBodyBytes is the configured body cap.
func (p *PushProcessor) MaxBodyBytes() int64 { return p.opts.MaxBodyBytes }

// Process verifies a push (getHeader looks up request headers), claims from
// its queue, runs the claimed tasks, and acknowledges them under their lease.
// Task execution is detached from ctx cancellation so a dropped connection
// does not abandon claimed work mid-handler.
func (p *PushProcessor) Process(ctx context.Context, getHeader func(string) string, body []byte) PushResult {
	env, rejected := p.Accept(getHeader, body)
	if rejected != nil {
		return *rejected
	}
	return p.Run(ctx, env)
}

// Accept verifies the signature and envelope without claiming anything. A
// non-nil result is a rejection to return as-is.
func (p *PushProcessor) Accept(getHeader func(string) string, body []byte) (*PushEnvelope, *PushResult) {
	if p.opts.Secret != "" {
		if err := verifyPushAt(p.opts.Secret, getHeader(PushTimestampHeader), getHeader(PushSignatureHeader), body, p.opts.Tolerance, p.opts.now()); err != nil {
			return nil, &PushResult{Status: http.StatusUnauthorized, Error: err.Error()}
		}
	}
	env, err := ParsePushEnvelope(body)
	if err != nil {
		return nil, &PushResult{Status: http.StatusBadRequest, Error: err.Error()}
	}
	if _, ok := p.handlers[env.HandlerName]; !ok {
		return nil, &PushResult{Status: http.StatusUnprocessableEntity, Error: fmt.Sprintf("no handler registered for %q", env.HandlerName)}
	}
	if env.QueueName == "" {
		return nil, &PushResult{Status: http.StatusUnprocessableEntity, Error: "push envelope has no queue_name to claim from"}
	}
	return env, nil
}

// Run claims from an accepted envelope's queue and executes what it claimed.
func (p *PushProcessor) Run(ctx context.Context, env *PushEnvelope) PushResult {
	batch, err := p.client.PollTaskBatchFromQueue(ctx, env.QueueName, env.HandlerName, p.opts.WorkerID, p.opts.MaxClaim)
	if err != nil {
		return PushResult{Status: http.StatusBadGateway, Error: "claim failed: " + err.Error()}
	}
	result := PushResult{Status: http.StatusOK, Claimed: make([]string, 0, len(batch.Tasks))}
	if len(batch.Tasks) == 0 {
		return result
	}
	execCtx := context.WithoutCancel(ctx)
	w := NewWorker(WorkerConfig{
		Client:            p.client,
		WorkerID:          p.opts.WorkerID,
		Handlers:          p.handlers,
		HeartbeatInterval: p.opts.HeartbeatInterval,
		MaxConcurrent:     len(batch.Tasks),
		OnTaskComplete:    p.opts.OnTaskComplete,
		OnTaskFail:        p.opts.OnTaskFail,
		Logger:            p.logger,
	})
	w.mu.Lock()
	w.applyHintsLocked(env.HandlerName, batch)
	w.mu.Unlock()
	hbCtx, stopHeartbeats := context.WithCancel(execCtx)
	defer stopHeartbeats()
	go w.heartbeatLoop(hbCtx)
	var wg sync.WaitGroup
	for _, task := range batch.Tasks {
		result.Claimed = append(result.Claimed, task.ID)
		wg.Add(1)
		go func(task WorkerTask) {
			defer wg.Done()
			w.executeTask(execCtx, task)
		}(task)
	}
	wg.Wait()
	return result
}

// NewPushHandler returns a net/http handler for push-mode queues. It returns
// an error when no secret is configured and AllowUnsigned is false.
func NewPushHandler(client *Client, handlers map[string]HandlerFunc, opts PushHandlerOptions) (http.Handler, error) {
	p, err := NewPushProcessor(client, handlers, opts)
	if err != nil {
		return nil, err
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writePushResult(w, PushResult{Status: http.StatusMethodNotAllowed, Error: "method not allowed"})
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, p.opts.MaxBodyBytes))
		if err != nil {
			writePushResult(w, PushResult{Status: http.StatusRequestEntityTooLarge, Error: "body too large or unreadable"})
			return
		}
		env, rejected := p.Accept(r.Header.Get, body)
		if rejected != nil {
			writePushResult(w, *rejected)
			return
		}
		if p.opts.Async {
			go p.Run(context.WithoutCancel(r.Context()), env)
			writePushResult(w, PushResult{Status: http.StatusAccepted})
			return
		}
		writePushResult(w, p.Run(r.Context(), env))
	}), nil
}

func writePushResult(w http.ResponseWriter, res PushResult) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.Status)
	_ = json.NewEncoder(w).Encode(res)
}
