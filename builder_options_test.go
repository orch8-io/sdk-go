package orch8

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStepWithEncodesContractFields(t *testing.T) {
	b := Workflow("parity").
		InputSchema(map[string]any{"type": "object"}).
		StepWith("charge", "charge", map[string]any{"cents": 100}, StepOptions{
			When:         `data.plan == "pro"`,
			Retry:        &RetryPolicy{MaxAttempts: 3, InitialBackoff: 1000, MaxBackoff: 60000, RetryIf: "error.retryable", NonRetryableCodes: []string{"card_declined"}},
			OutputSchema: map[string]any{"type": "object", "required": []string{"charge_id"}},
			Delay:        &DelaySpec{FireAtLocal: "2026-03-08T09:00:00", Timezone: "America/New_York"},
			Compensation: &StepCompensation{Handler: "refund", Verification: "provider_receipt"},
			Cancellable:  Ptr(false),
		}).
		OnFailure(func(f *WorkflowBuilder) { f.Step("alert", "notify", nil) })
	if err := b.Err(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(b.Build())
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		InputSchema map[string]any   `json:"input_schema"`
		OnFailure   []map[string]any `json:"on_failure"`
		Blocks      []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	step := decoded.Blocks[0]
	retry := step["retry"].(map[string]any)
	delay := step["delay"].(map[string]any)
	if step["when"] != `data.plan == "pro"` || retry["retry_if"] != "error.retryable" ||
		retry["non_retryable_codes"].([]any)[0] != "card_declined" || step["output_schema"] == nil ||
		delay["timezone"] != "America/New_York" || delay["duration"] != float64(0) ||
		step["compensation"].(map[string]any)["handler"] != "refund" || step["cancellable"] != false {
		t.Fatalf("unexpected step encoding: %s", data)
	}
	if decoded.InputSchema["type"] != "object" || len(decoded.OnFailure) != 1 {
		t.Fatalf("missing sequence-level fields: %s", data)
	}
	if strings.Contains(string(data), "on_cancel") {
		t.Fatalf("empty on_cancel should be omitted: %s", data)
	}
}

func TestStepWithValidation(t *testing.T) {
	cases := []StepOptions{
		{Retry: &RetryPolicy{MaxAttempts: 0}},
		{Delay: &DelaySpec{FireAtLocal: "2026-01-01T09:00:00"}},
		{Compensation: &StepCompensation{Handler: "x", Verification: "vibes"}},
	}
	for i, opts := range cases {
		if Workflow("bad").StepWith("s", "h", nil, opts).Err() == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
	// Errors inside nested branches propagate.
	nested := Workflow("nested").Parallel("p", func(b *WorkflowBuilder) {
		b.StepWith("s", "h", nil, StepOptions{Retry: &RetryPolicy{}})
	})
	if nested.Err() == nil {
		t.Fatal("expected nested branch error to propagate")
	}
}

func TestLoopForEachRaceOptions(t *testing.T) {
	b := Workflow("loops").
		LoopWith("poll", "data.pending", func(l *WorkflowBuilder) { l.Step("check", "check", nil) },
			LoopOptions{MaxIterations: 50, BreakOn: "data.done", RetainIterations: Ptr(5)}).
		ForEachWith("each", "data.items", func(l *WorkflowBuilder) { l.Step("do", "do", nil) },
			ForEachOptions{ItemVar: "item", RetainIterations: Ptr(0)}).
		RaceWith("fast", RaceFirstToSucceed, func(r *WorkflowBuilder) { r.Step("a", "a", nil) }).
		Delay("wait", DelaySpec{Duration: 1000})
	if err := b.Err(); err != nil {
		t.Fatal(err)
	}
	blocks := b.Build().Blocks
	if blocks[0]["retain_iterations"] != 5 || blocks[0]["break_on"] != "data.done" || blocks[1]["retain_iterations"] != 0 ||
		blocks[2]["semantics"] != RaceFirstToSucceed || blocks[3]["handler"] != "noop" {
		t.Fatalf("unexpected blocks: %#v", blocks)
	}
	if Workflow("x").RaceWith("r", "whoever", nil).Err() == nil {
		t.Fatal("expected invalid race semantics error")
	}
}
