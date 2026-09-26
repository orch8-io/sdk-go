package orch8

import (
	"context"
	"net/http"
	"net/url"
)

// JSONObject is an open JSON object used by the experimental continuity
// surface, whose full schemas are published through the engine's OpenAPI.
type JSONObject = map[string]any

// ContinuityClient wraps the basics of the engine's portable-continuity
// control plane: executions, runtimes, handoffs, grants, effects, provenance,
// and placement. Bodies stay open JSON while the surface evolves; use
// Client.Request for routes not wrapped here.
type ContinuityClient struct{ c *Client }

// Continuity returns the continuity sub-client.
func (c *Client) Continuity() *ContinuityClient { return &ContinuityClient{c: c} }

func tenantQuery(tenantID string, extra ...string) string {
	values := url.Values{}
	if tenantID != "" {
		values.Set("tenant_id", tenantID)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if extra[i+1] != "" {
			values.Set(extra[i], extra[i+1])
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

func (cc *ContinuityClient) post(ctx context.Context, path string, body any) (JSONObject, error) {
	var out JSONObject
	err := cc.c.do(ctx, http.MethodPost, path, body, &out)
	return out, err
}

func (cc *ContinuityClient) getObject(ctx context.Context, path string) (JSONObject, error) {
	var out JSONObject
	err := cc.c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

func (cc *ContinuityClient) getList(ctx context.Context, path string) ([]JSONObject, error) {
	var out []JSONObject
	err := cc.c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

// CreateExecution registers a portable execution.
func (cc *ContinuityClient) CreateExecution(ctx context.Context, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/executions", body)
}

// GetExecution fetches a portable execution.
func (cc *ContinuityClient) GetExecution(ctx context.Context, id, tenantID string) (JSONObject, error) {
	return cc.getObject(ctx, "/continuity/executions/"+pathSegment(id)+tenantQuery(tenantID))
}

// ListLocations lists where an execution has run.
func (cc *ContinuityClient) ListLocations(ctx context.Context, id, tenantID string) ([]JSONObject, error) {
	return cc.getList(ctx, "/continuity/executions/"+pathSegment(id)+"/locations"+tenantQuery(tenantID))
}

// RegisterRuntime registers runtime capabilities.
func (cc *ContinuityClient) RegisterRuntime(ctx context.Context, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/runtimes/register", body)
}

// ListRuntimes lists registered runtimes for a tenant.
func (cc *ContinuityClient) ListRuntimes(ctx context.Context, tenantID string) ([]JSONObject, error) {
	return cc.getList(ctx, "/runtimes"+tenantQuery(tenantID))
}

// PreviewHandoff previews moving an execution to another runtime.
func (cc *ContinuityClient) PreviewHandoff(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/executions/"+pathSegment(id)+"/handoff-preview", body)
}

// CreateHandoff starts a handoff.
func (cc *ContinuityClient) CreateHandoff(ctx context.Context, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/handoffs", body)
}

// GetHandoff fetches a handoff.
func (cc *ContinuityClient) GetHandoff(ctx context.Context, id, tenantID string) (JSONObject, error) {
	return cc.getObject(ctx, "/continuity/handoffs/"+pathSegment(id)+tenantQuery(tenantID))
}

func (cc *ContinuityClient) handoffAction(ctx context.Context, id, action string, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/handoffs/"+pathSegment(id)+"/"+action, body)
}

// ExportHandoff exports a handoff capsule.
func (cc *ContinuityClient) ExportHandoff(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.handoffAction(ctx, id, "export", body)
}

// AcceptHandoff accepts a handoff on the target runtime.
func (cc *ContinuityClient) AcceptHandoff(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.handoffAction(ctx, id, "accept", body)
}

// RejectHandoff rejects a handoff.
func (cc *ContinuityClient) RejectHandoff(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.handoffAction(ctx, id, "reject", body)
}

// ResumeHandoff resumes an accepted handoff.
func (cc *ContinuityClient) ResumeHandoff(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.handoffAction(ctx, id, "resume", body)
}

// RevokeHandoff revokes a pending handoff.
func (cc *ContinuityClient) RevokeHandoff(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.handoffAction(ctx, id, "revoke", body)
}

// ImportCapsule imports an exported capsule.
func (cc *ContinuityClient) ImportCapsule(ctx context.Context, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/capsules/import", body)
}

// IssueGrant issues a continuation grant.
func (cc *ContinuityClient) IssueGrant(ctx context.Context, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/grants", body)
}

// ConsumeGrant consumes a continuation grant.
func (cc *ContinuityClient) ConsumeGrant(ctx context.Context, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/grants/consume", body)
}

// ListEffects lists the external effects recorded for an execution.
func (cc *ContinuityClient) ListEffects(ctx context.Context, id, tenantID string) ([]JSONObject, error) {
	return cc.getList(ctx, "/continuity/executions/"+pathSegment(id)+"/effects"+tenantQuery(tenantID))
}

// ResolveEffect resolves an effect whose outcome is unknown.
func (cc *ContinuityClient) ResolveEffect(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/effects/"+pathSegment(id)+"/resolve", body)
}

// ListProvenance lists an execution's provenance chain.
func (cc *ContinuityClient) ListProvenance(ctx context.Context, id, tenantID string) ([]JSONObject, error) {
	return cc.getList(ctx, "/continuity/executions/"+pathSegment(id)+"/provenance"+tenantQuery(tenantID))
}

// RecordProvenance appends a provenance boundary.
func (cc *ContinuityClient) RecordProvenance(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/executions/"+pathSegment(id)+"/provenance", body)
}

// VerifyProvenance verifies the provenance chain, optionally against an
// expected head hash.
func (cc *ContinuityClient) VerifyProvenance(ctx context.Context, id, tenantID, expectedHead string) (JSONObject, error) {
	return cc.getObject(ctx, "/continuity/executions/"+pathSegment(id)+"/provenance/verify"+tenantQuery(tenantID, "expected_head", expectedHead))
}

// ChoosePlacement asks the engine to pick a runtime for an execution.
func (cc *ContinuityClient) ChoosePlacement(ctx context.Context, id string, body JSONObject) (JSONObject, error) {
	return cc.post(ctx, "/continuity/executions/"+pathSegment(id)+"/placement", body)
}
