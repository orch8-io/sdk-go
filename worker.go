package orch8

import (
	"context"
	"log"
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

	cancel   context.CancelFunc
	sem      chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	inflight map[string]struct{}
	backoff  map[string]time.Duration
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
		sem:                 make(chan struct{}, maxConcurrent),
		inflight:            make(map[string]struct{}),
		backoff:             make(map[string]time.Duration),
	}
}

// Start begins polling for tasks and blocks until the context is cancelled or Stop is called.
func (w *Worker) Start(ctx context.Context) {
	ctx, w.cancel = context.WithCancel(ctx)

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
}

// Stop signals the worker to stop polling and waits for in-flight tasks to complete.
func (w *Worker) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *Worker) pollLoop(ctx context.Context, handlerName string) {
	// Immediate first poll.
	w.poll(ctx, handlerName)

	for {
		w.mu.Lock()
		interval := w.backoff[handlerName]
		w.mu.Unlock()
		if interval == 0 {
			interval = w.pollInterval
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

	tasks, err := w.client.PollTasks(ctx, handlerName, w.workerID, limit)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("orch8: poll error for handler %s: %v", handlerName, err)
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

	// Reset backoff on successful poll.
	w.mu.Lock()
	delete(w.backoff, handlerName)
	w.mu.Unlock()

	if len(tasks) == 0 {
		return
	}

	for _, task := range tasks {
		task := task
		// Acquire semaphore slot.
		select {
		case w.sem <- struct{}{}:
		case <-ctx.Done():
			return
		}

		w.mu.Lock()
		w.inflight[task.ID] = struct{}{}
		w.mu.Unlock()

		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			defer func() {
				<-w.sem
				w.mu.Lock()
				delete(w.inflight, task.ID)
				w.mu.Unlock()
			}()
			w.executeTask(ctx, task)
		}()
	}
}

func (w *Worker) executeTask(ctx context.Context, task WorkerTask) {
	handler, ok := w.handlers[task.HandlerName]
	if !ok {
		if err := w.client.FailTask(ctx, task.ID, w.workerID, "no handler registered for \""+task.HandlerName+"\"", false); err != nil {
			log.Printf("orch8: failed to report missing handler for task %s: %v", task.ID, err)
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
		// Handler exceptions are non-retryable by default — matches engine
		// FailRequest default and harmonizes behavior across SDKs. Callers
		// that need retry-on-error can invoke FailTask directly from the
		// handler with retryable=true.
		if failErr := w.client.FailTask(ctx, task.ID, w.workerID, err.Error(), false); failErr != nil {
			log.Printf("orch8: failed to report failure for task %s: %v", task.ID, failErr)
		}
		if w.onTaskFail != nil {
			w.onTaskFail(task, err)
		}
		return
	}

	if output == nil {
		output = map[string]any{}
	}
	if err := w.client.CompleteTask(ctx, task.ID, w.workerID, output); err != nil {
		log.Printf("orch8: failed to report completion for task %s: %v", task.ID, err)
	}
	if w.onTaskComplete != nil {
		w.onTaskComplete(task, output)
	}
}

func (w *Worker) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.mu.Lock()
			ids := make([]string, 0, len(w.inflight))
			for id := range w.inflight {
				ids = append(ids, id)
			}
			w.mu.Unlock()

			for _, id := range ids {
				if err := w.client.HeartbeatTask(ctx, id, w.workerID); err != nil && ctx.Err() == nil {
					log.Printf("orch8: heartbeat error for task %s: %v", id, err)
				}
			}
		}
	}
}
