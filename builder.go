package orch8

import "fmt"

// Block is one node in the versioned Orch8 workflow DSL.
type Block map[string]any

// WorkflowDefinition is the authoring payload accepted by CreateSequence.
type WorkflowDefinition struct {
	Name        string  `json:"name"`
	Namespace   string  `json:"namespace"`
	Blocks      []Block `json:"blocks"`
	InputSchema any     `json:"input_schema,omitempty"`
	OnFailure   []Block `json:"on_failure,omitempty"`
	OnCancel    []Block `json:"on_cancel,omitempty"`
}

// WorkflowBuilder builds nested block trees without stringly-typed map assembly.
type WorkflowBuilder struct {
	name        string
	namespace   string
	blocks      []Block
	inputSchema any
	onFailure   []Block
	onCancel    []Block
	err         error
}

type Branch func(*WorkflowBuilder)

func Workflow(name string) *WorkflowBuilder {
	return &WorkflowBuilder{name: name, namespace: "default"}
}

func (b *WorkflowBuilder) Namespace(namespace string) *WorkflowBuilder {
	b.namespace = namespace
	return b
}

func (b *WorkflowBuilder) Step(id, handler string, params any, options ...map[string]any) *WorkflowBuilder {
	block := Block{"type": "step", "id": id, "handler": handler, "params": params}
	if params == nil {
		block["params"] = map[string]any{}
	}
	for _, option := range options {
		for key, value := range option {
			block[key] = value
		}
	}
	return b.Raw(block)
}

// TypedStep preserves the concrete parameter type at the call site.
func TypedStep[P any](b *WorkflowBuilder, id, handler string, params P, options ...map[string]any) *WorkflowBuilder {
	return b.Step(id, handler, params, options...)
}

func (b *WorkflowBuilder) Parallel(id string, branches ...Branch) *WorkflowBuilder {
	return b.Raw(Block{"type": "parallel", "id": id, "branches": b.branches(branches)})
}

func (b *WorkflowBuilder) Race(id string, branches ...Branch) *WorkflowBuilder {
	return b.Raw(Block{"type": "race", "id": id, "branches": b.branches(branches)})
}

func (b *WorkflowBuilder) Loop(id, condition string, body Branch, maxIterations int) *WorkflowBuilder {
	return b.Raw(Block{"type": "loop", "id": id, "condition": condition, "body": b.branch(body), "max_iterations": maxIterations})
}

func (b *WorkflowBuilder) ForEach(id, collection, itemVar string, body Branch) *WorkflowBuilder {
	return b.Raw(Block{"type": "for_each", "id": id, "collection": collection, "item_var": itemVar, "body": b.branch(body)})
}

type Route struct {
	Condition string
	Blocks    Branch
}

func (b *WorkflowBuilder) Router(id string, routes []Route, fallback Branch) *WorkflowBuilder {
	resolved := make([]Block, 0, len(routes))
	for _, route := range routes {
		resolved = append(resolved, Block{"condition": route.Condition, "blocks": b.branch(route.Blocks)})
	}
	block := Block{"type": "router", "id": id, "routes": resolved}
	if fallback != nil {
		block["default"] = b.branch(fallback)
	}
	return b.Raw(block)
}

func (b *WorkflowBuilder) TryCatch(id string, tryBlock, catchBlock, finallyBlock Branch) *WorkflowBuilder {
	block := Block{"type": "try_catch", "id": id, "try_block": b.branch(tryBlock), "catch_block": b.branch(catchBlock)}
	if finallyBlock != nil {
		block["finally_block"] = b.branch(finallyBlock)
	}
	return b.Raw(block)
}

func (b *WorkflowBuilder) SubSequence(id, name string, version *int, input any) *WorkflowBuilder {
	block := Block{"type": "sub_sequence", "id": id, "sequence_name": name}
	if version != nil {
		block["version"] = *version
	}
	if input != nil {
		block["input"] = input
	}
	return b.Raw(block)
}

type ABVariant struct {
	Name   string
	Weight int
	Blocks Branch
}

func (b *WorkflowBuilder) ABSplit(id string, variants ...ABVariant) *WorkflowBuilder {
	resolved := make([]Block, 0, len(variants))
	for _, variant := range variants {
		resolved = append(resolved, Block{"name": variant.Name, "weight": variant.Weight, "blocks": b.branch(variant.Blocks)})
	}
	return b.Raw(Block{"type": "ab_split", "id": id, "variants": resolved})
}

func (b *WorkflowBuilder) CancellationScope(id string, body Branch) *WorkflowBuilder {
	return b.Raw(Block{"type": "cancellation_scope", "id": id, "blocks": b.branch(body)})
}

type SagaStep struct {
	ID           string
	Action       Branch
	Compensation Branch
}

func (b *WorkflowBuilder) Saga(id string, steps ...SagaStep) *WorkflowBuilder {
	if b.err != nil {
		return b
	}
	resolved := make([]Block, 0, len(steps))
	for _, step := range steps {
		action := b.branch(step.Action)
		compensation := b.branch(step.Compensation)
		if len(action) != 1 || len(compensation) > 1 {
			b.err = fmt.Errorf("saga step %s requires one action and at most one compensation", step.ID)
			return b
		}
		item := Block{"id": step.ID, "action": action[0]}
		if len(compensation) == 1 {
			item["compensation"] = compensation[0]
		}
		resolved = append(resolved, item)
	}
	return b.Raw(Block{"type": "saga", "id": id, "steps": resolved})
}

func (b *WorkflowBuilder) Raw(block Block) *WorkflowBuilder {
	b.blocks = append(b.blocks, block)
	return b
}

func (b *WorkflowBuilder) Build() WorkflowDefinition {
	return WorkflowDefinition{
		Name:        b.name,
		Namespace:   b.namespace,
		Blocks:      append([]Block(nil), b.blocks...),
		InputSchema: b.inputSchema,
		OnFailure:   append([]Block(nil), b.onFailure...),
		OnCancel:    append([]Block(nil), b.onCancel...),
	}
}

// Err reports a builder-shape error, such as a saga action containing more
// than one block. Call it before submitting Build() to the API.
func (b *WorkflowBuilder) Err() error {
	return b.err
}

func (b *WorkflowBuilder) branch(branch Branch) []Block {
	if branch == nil {
		return nil
	}
	inner := &WorkflowBuilder{name: "_inner", namespace: b.namespace}
	branch(inner)
	if inner.err != nil && b.err == nil {
		b.err = inner.err
	}
	return inner.blocks
}

func (b *WorkflowBuilder) branches(branches []Branch) [][]Block {
	result := make([][]Block, 0, len(branches))
	for _, branch := range branches {
		result = append(result, b.branch(branch))
	}
	return result
}
