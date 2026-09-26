package orch8

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// HandlerFunc is a function that processes a worker task and returns an output or error.
type HandlerFunc func(ctx context.Context, task WorkerTask) (any, error)

// WorkerConfig holds configuration for the polling worker.
type WorkerConfig struct {
	Client            *Client
	WorkerID          string
	Handlers          map[string]HandlerFunc
	PollInterval      time.Duration
	HeartbeatInterval time.Duration
	MaxConcurrent     int

	// CircuitBreakerCheck enables checking circuit breaker state before
	// polling each handler. When the breaker is "open" the handler is skipped.
	CircuitBreakerCheck bool

	// OnTaskComplete is called after a task is successfully completed.
	OnTaskComplete func(task WorkerTask, output any)
	// OnTaskFail is called after a task fails.
	OnTaskFail func(task WorkerTask, err error)

	// Logger is used for worker log output. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// WorkerRuntimeStats is a language-neutral snapshot of worker capacity.
type WorkerRuntimeStats struct {
	Running        bool
	InFlight       int
	AvailableSlots int
	Handlers       []string
}

// maxBackoff is the upper bound for exponential backoff on poll failures.
const maxBackoff = 30 * time.Second

// Worker is a polling worker that claims and executes tasks from the Orch8 engine.
type Worker struct {
	client            *Client
	workerID          string
	handlers          map[string]HandlerFunc
	pollInterval      time.Duration
	heartbeatInterval time.Duration
	maxConcurrent     int

	circuitBreakerCheck bool
	onTaskComplete      func(task WorkerTask, output any)
	onTaskFail          func(task WorkerTask, err error)
	logger              *slog.Logger

	cancel   context.CancelFunc
	running  bool
	sem      chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	inflight map[string]WorkerTask
	backoff  map[string]time.Duration
	// pollHints holds the server's minimum delay before the next poll.
	pollHints map[string]time.Duration
	// leaseHeartbeat is the heartbeat interval derived from poll hints
	// (min of advertised interval and half the lease); zero when unknown.
	leaseHeartbeat time.Duration
	hbReset        chan struct{}
}

// NewWorker creates a new polling worker.
func NewWorker(cfg WorkerConfig) *Worker {
	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = time.Second
	}
	heartbeatInterval := cfg.HeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = 15 * time.Second
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 10
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Worker{
		client:              cfg.Client,
		workerID:            cfg.WorkerID,
		handlers:            cfg.Handlers,
		pollInterval:        pollInterval,
		heartbeatInterval:   heartbeatInterval,
		maxConcurrent:       maxConcurrent,
		circuitBreakerCheck: cfg.CircuitBreakerCheck,
		onTaskComplete:      cfg.OnTaskComplete,
		onTaskFail:          cfg.OnTaskFail,
		logger:              logger,
		sem:                 make(chan struct{}, maxConcurrent),
		inflight:            make(map[string]WorkerTask),
		backoff:             make(map[string]time.Duration),
		pollHints:           make(map[string]time.Duration),
		hbReset:             make(chan struct{}, 1),
	}
}

// Start begins polling for tasks and blocks until the context is cancelled or Stop is called.
func (w *Worker) Start(ctx context.Context) {
	w.mu.Lock()
	ctx, w.cancel = context.WithCancel(ctx)
	w.running = true
	w.mu.Unlock()

	// Start heartbeat goroutine.
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.heartbeatLoop(ctx)
	}()

	// Start a poll loop per handler.
	for name := range w.handlers {
		handlerName := name
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.pollLoop(ctx, handlerName)
		}()
	}

	// Block until context is done.
	<-ctx.Done()

	// Wait for all in-flight tasks to finish.
	w.wg.Wait()
	w.mu.Lock()
	w.running = false
	w.cancel = nil
	w.mu.Unlock()
}

// Stop signals the worker to stop polling and waits for in-flight tasks to complete.
func (w *Worker) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
}

// Stats returns the current worker capacity without exposing mutable internals.
func (w *Worker) Stats() WorkerRuntimeStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	handlers := make([]string, 0, len(w.handlers))
	for handler := range w.handlers {
		handlers = append(handlers, handler)
	}
	inFlight := len(w.inflight)
	return WorkerRuntimeStats{
		Running:        w.running,
		InFlight:       inFlight,
		AvailableSlots: w.maxConcurrent - inFlight,
		Handlers:       handlers,
	}
}

func (w *Worker) pollLoop(ctx context.Context, handlerName string) {
	// Immediate first poll.
	w.poll(ctx, handlerName)

	for {
		w.mu.Lock()
		interval := w.backoff[handlerName]
		hint := w.pollHints[handlerName]
		w.mu.Unlock()
		if interval == 0 {
			interval = w.pollInterval
		}
		if hint > interval {
			interval = hint
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			w.poll(ctx, handlerName)
		}
	}
}

func (w *Worker) poll(ctx context.Context, handlerName string) {
	if ctx.Err() != nil {
		return
	}

	// Check circuit breaker if enabled.
	if w.circuitBreakerCheck {
		cb, err := w.client.GetCircuitBreaker(ctx, handlerName)
		if err == nil && cb.State == "open" {
			return
		}
	}

	// Calculate how many slots are available.
	limit := w.maxConcurrent - len(w.sem)
	if limit <= 0 {
		return
	}

	batch, err := w.client.PollTaskBatch(ctx, handlerName, w.workerID, limit)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Error("poll error", "handler", handlerName, "error", err)
		}
		// Exponential backoff on failure.
		w.mu.Lock()
		cur := w.backoff[handlerName]
		if cur == 0 {
			cur = w.pollInterval
		}
		cur *= 2
		if cur > maxBackoff {
			cur = maxBackoff
		}
		w.backoff[handlerName] = cur
		w.mu.Unlock()
		return
	}

	// Reset backoff on successful poll and record the server's timing hints.
	w.mu.Lock()
	delete(w.backoff, handlerName)
	w.applyHintsLocked(handlerName, batch)
	w.mu.Unlock()
	tasks := batch.Tasks

	if len(tasks) == 0 {
		return
	}

	for _, task := range tasks {
		task := task
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.executeTask(ctx, task)
		}()
	}
}

func (w *Worker) executeTask(ctx context.Context, task WorkerTask) {
	// Acquire semaphore slot.
	select {
	case w.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() {
		<-w.sem
		w.mu.Lock()
		delete(w.inflight, task.ID)
		w.mu.Unlock()
	}()

	w.mu.Lock()
	w.inflight[task.ID] = task
	w.mu.Unlock()

	lease := task.Lease(w.workerID)
	handler, ok := w.handlers[task.HandlerName]
	if !ok {
		if err := w.client.FailTaskLease(ctx, task.ID, lease, "no handler registered for \""+task.HandlerName+"\"", false); err != nil {
			w.logger.Error("failed to report missing handler", "task", task.ID, "error", err)
		}
		return
	}

	// Apply timeout if specified.
	taskCtx := ctx
	if task.TimeoutMs != nil && *task.TimeoutMs > 0 {
		var cancel context.CancelFunc
		taskCtx, cancel = context.WithTimeout(ctx, time.Duration(*task.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	output, err := handler(taskCtx, task)
	if err != nil {
		retryable := false
		if rerr, ok := err.(interface{ Retryable() bool }); ok {
			retryable = rerr.Retryable()
		}
		if failErr := w.client.FailTaskLease(ctx, task.ID, lease, err.Error(), retryable); failErr != nil {
			// Leave the task for lease recovery; the failure was not acknowledged.
			w.logger.Error("failed to report failure", "task", task.ID, "error", failErr)
			return
		}
		if w.onTaskFail != nil {
			w.notify(func() { w.onTaskFail(task, err) })
		}
		return
	}

	if output == nil {
		output = map[string]any{}
	}
	if err := w.client.CompleteTaskLease(ctx, task.ID, lease, output); err != nil {
		// A rejected or ambiguous acknowledgement is not a handler failure:
		// never send a contradictory fail, and never report success.
		w.logger.Error("failed to report completion", "task", task.ID, "error", err)
		return
	}
	if w.onTaskComplete != nil {
		w.notify(func() { w.onTaskComplete(task, output) })
	}
}

func (w *Worker) notify(callback func()) {
	defer func() { _ = recover() }()
	callback()
}

func (w *Worker) applyHintsLocked(handlerName string, batch *PollBatch) {
	if batch.PollAfterMs != nil {
		w.pollHints[handlerName] = time.Duration(*batch.PollAfterMs) * time.Millisecond
	} else {
		delete(w.pollHints, handlerName)
	}
	hb := time.Duration(0)
	if batch.HeartbeatIntervalSecs != nil && *batch.HeartbeatIntervalSecs > 0 {
		hb = time.Duration(*batch.HeartbeatIntervalSecs) * time.Second
	}
	if batch.LeaseSecs != nil && *batch.LeaseSecs > 0 {
		half := time.Duration(*batch.LeaseSecs) * time.Second / 2
		if hb == 0 || half < hb {
			hb = half
		}
	}
	if hb > 0 && (w.leaseHeartbeat == 0 || hb < w.leaseHeartbeat) {
		w.leaseHeartbeat = hb
		select {
		case w.hbReset <- struct{}{}:
		default:
		}
	}
}

// currentHeartbeatInterval is the configured interval, shortened to the
// server-advertised heartbeat interval or half the lease when smaller.
func (w *Worker) currentHeartbeatInterval() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	d := w.heartbeatInterval
	if w.leaseHeartbeat > 0 && w.leaseHeartbeat < d {
		d = w.leaseHeartbeat
	}
	return d
}

func (w *Worker) heartbeatLoop(ctx context.Context) {
	for {
		timer := time.NewTimer(w.currentHeartbeatInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-w.hbReset:
			// The lease-derived interval shrank; re-arm with the new value.
			timer.Stop()
			continue
		case <-timer.C:
			w.mu.Lock()
			tasks := make([]WorkerTask, 0, len(w.inflight))
			for _, task := range w.inflight {
				tasks = append(tasks, task)
			}
			w.mu.Unlock()

			for _, task := range tasks {
				if _, err := w.client.HeartbeatTaskLease(ctx, task.ID, task.Lease(w.workerID), nil, nil); err != nil && ctx.Err() == nil {
					w.logger.Error("heartbeat error", "task", task.ID, "error", err)
				}
			}
		}
	}
}
