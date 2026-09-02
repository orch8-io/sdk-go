package orch8

import "testing"

func TestWorkflowBuilderNestedBlocks(t *testing.T) {
	definition := Workflow("checkout").Parallel("notify",
		func(branch *WorkflowBuilder) {
			branch.Step("email", "send-email", map[string]string{"to": "a@example.com"})
		},
		func(branch *WorkflowBuilder) {
			branch.ABSplit("copy",
				ABVariant{Name: "a", Weight: 50, Blocks: func(v *WorkflowBuilder) { v.Step("a", "render", nil) }},
				ABVariant{Name: "b", Weight: 50, Blocks: func(v *WorkflowBuilder) { v.Step("b", "render", nil) }},
			)
		},
	).Build()
	if definition.Name != "checkout" || definition.Blocks[0]["type"] != "parallel" {
		t.Fatalf("unexpected definition: %#v", definition)
	}
}

func TestTypedStepAcceptsConcreteParams(t *testing.T) {
	type Charge struct {
		Cents int `json:"cents"`
	}
	builder := Workflow("typed")
	TypedStep(builder, "charge", "charge", Charge{Cents: 2500})
	if got := builder.Build().Blocks[0]["params"].(Charge).Cents; got != 2500 {
		t.Fatalf("got %d", got)
	}
}

func TestRouterPreservesRouteOrder(t *testing.T) {
	builder := Workflow("router").Router("choose", []Route{
		{Condition: "data.score > 90", Blocks: func(route *WorkflowBuilder) { route.Step("first", "notify", nil) }},
		{Condition: "data.score > 50", Blocks: func(route *WorkflowBuilder) { route.Step("second", "notify", nil) }},
	}, nil)
	routes := builder.Build().Blocks[0]["routes"].([]Block)
	if routes[0]["condition"] != "data.score > 90" {
		t.Fatalf("route priority changed: %#v", routes)
	}
}

func TestInvalidSagaRecordsErrorInsteadOfPanicking(t *testing.T) {
	builder := Workflow("saga").Saga("payment", SagaStep{
		ID: "charge",
		Action: func(action *WorkflowBuilder) {
			action.Step("one", "charge", nil).Step("two", "receipt", nil)
		},
	})
	if builder.Err() == nil {
		t.Fatal("expected invalid saga to record a builder error")
	}
}
