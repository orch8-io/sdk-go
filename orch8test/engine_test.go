package orch8test_test

import (
	"context"
	"errors"
	"testing"
	"time"

	orch8 "github.com/orch8-io/sdk-go"
	"github.com/orch8-io/sdk-go/orch8test"
)

type retryableErr struct{ msg string }

func (e retryableErr) Error() string   { return e.msg }
func (e retryableErr) Retryable() bool { return true }

func startWorker(t *testing.T, eng *orch8test.Engine, handlers map[string]orch8.HandlerFunc) {
	t.Helper()
	w := orch8.NewWorker(orch8.WorkerConfig{
		Client: eng.Client(), WorkerID: "test-worker", Handlers: handlers,
		PollInterval: 5 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Start(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func TestWorkerCompletesAndFailsTasks(t *testing.T) {
	eng := orch8test.NewEngine(t)
	ok := eng.EnqueueTask("greet", map[string]any{"name": "ada"})
	bad := eng.EnqueueTask("explode", nil)
	startWorker(t, eng, map[string]orch8.HandlerFunc{
		"greet": func(_ context.Context, task orch8.WorkerTask) (any, error) {
			return map[string]any{"hello": task.Params.(map[string]any)["name"]}, nil
		},
		"explode": func(context.Context, orch8.WorkerTask) (any, error) { return nil, errors.New("boom") },
	})
	rec := eng.WaitForTask(t, ok.ID, 5*time.Second)
	if rec.State != orch8test.StateCompleted || rec.Output.(map[string]any)["hello"] != "ada" || rec.ClaimEpoch != 1 {
		t.Fatalf("unexpected record %+v", rec)
	}
	failed := eng.WaitForTask(t, bad.ID, 5*time.Second)
	if failed.State != orch8test.StateFailed || failed.FailMessage != "boom" || failed.FailRetryable {
		t.Fatalf("unexpected failure %+v", failed)
	}
}

func TestStaleLeaseIsRejected(t *testing.T) {
	eng := orch8test.NewEngine(t)
	task := eng.EnqueueTask("slow", nil)
	c := eng.Client()
	ctx := context.Background()
	batch, err := c.PollTaskBatch(ctx, "slow", "w1", 1)
	if err != nil || len(batch.Tasks) != 1 || *batch.LeaseSecs != 30 {
		t.Fatal(batch, err)
	}
	old := batch.Tasks[0]
	eng.ExpireLease(task.ID)
	fresh, _ := c.PollTasks(ctx, "slow", "w2", 1)
	if err := c.CompleteTaskLease(ctx, old.ID, old.Lease("w1"), nil); err == nil {
		t.Fatal("stale lease completion should be rejected")
	}
	if err := c.CompleteTaskLease(ctx, fresh[0].ID, fresh[0].Lease("w2"), map[string]any{"ok": 1}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := eng.Task(task.ID); rec.StaleRejections != 1 || rec.ClaimEpoch != 3 {
		t.Fatalf("unexpected record %+v", rec)
	}
}

func TestInstancesRoundTrip(t *testing.T) {
	eng := orch8test.NewEngine(t)
	c := eng.Client(orch8.ClientConfig{TenantID: "acme"})
	inst, err := c.CreateInstance(context.Background(), map[string]any{"sequence_id": "seq-1", "context": map[string]any{"data": map[string]any{"x": 1}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.GetInstance(context.Background(), inst.ID)
	if err != nil || got.SequenceID != "seq-1" || got.TenantID != "acme" || got.State != "scheduled" {
		t.Fatal(got, err)
	}
}

func TestJobsEndToEndWithRetriesAndDelay(t *testing.T) {
	eng := orch8test.NewEngine(t)
	c := eng.Client()
	ctx := context.Background()
	attempts := 0
	startWorker(t, eng, map[string]orch8.HandlerFunc{
		"flaky": func(context.Context, orch8.WorkerTask) (any, error) {
			attempts++
			if attempts < 2 {
				return nil, retryableErr{"try again"}
			}
			return map[string]any{"attempts": attempts}, nil
		},
		"later": func(context.Context, orch8.WorkerTask) (any, error) { return nil, nil },
	})
	job, err := c.Jobs.Enqueue(ctx, orch8.EnqueueJobRequest{Handler: "flaky", Retry: &orch8.JobRetry{MaxAttempts: 3, InitialBackoffMs: 1}, IdempotencyKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := c.Jobs.Enqueue(ctx, orch8.EnqueueJobRequest{Handler: "flaky", IdempotencyKey: "k"})
	if again.ID != job.ID {
		t.Fatal("idempotency key should return the original job")
	}
	done, err := c.Jobs.WaitFor(ctx, job.ID, orch8.WaitOptions{Interval: 5 * time.Millisecond, Timeout: 5 * time.Second})
	if err != nil || done.Status != orch8.JobCompleted || len(done.Attempts) != 2 || done.Attempts[0].Status != orch8.JobFailed {
		t.Fatalf("unexpected job %+v err=%v", done, err)
	}

	delayed, _ := c.Jobs.Enqueue(ctx, orch8.EnqueueJobRequest{Handler: "later", DelayMs: orch8.Ptr(int64(time.Hour / time.Millisecond))})
	time.Sleep(50 * time.Millisecond)
	if j, _ := c.Jobs.Get(ctx, delayed.ID); j.Status != orch8.JobScheduled {
		t.Fatalf("delayed job ran early: %s", j.Status)
	}
	eng.Advance(time.Hour)
	if j, err := c.Jobs.WaitFor(ctx, delayed.ID, orch8.WaitOptions{Interval: 5 * time.Millisecond, Timeout: 5 * time.Second}); err != nil || j.Status != orch8.JobCompleted {
		t.Fatal(j, err)
	}

	cancelMe, _ := c.Jobs.Enqueue(ctx, orch8.EnqueueJobRequest{Handler: "later", DelayMs: orch8.Ptr(int64(60000))})
	if err := c.Jobs.Cancel(ctx, cancelMe.ID); err != nil {
		t.Fatal(err)
	}
	var ids []string
	it := c.Jobs.List(ctx, orch8.JobListOptions{Limit: 1})
	for it.Next() {
		ids = append(ids, it.Job().Status)
	}
	if it.Err() != nil || len(ids) != 3 || ids[2] != orch8.JobCancelled {
		t.Fatal(ids, it.Err())
	}
}
