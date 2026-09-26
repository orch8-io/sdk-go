package lambda

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
	orch8 "github.com/orch8-io/sdk-go"
	"github.com/orch8-io/sdk-go/orch8test"
)

func handlers() map[string]orch8.HandlerFunc {
	return map[string]orch8.HandlerFunc{
		"thumb": func(context.Context, orch8.WorkerTask) (any, error) { return map[string]any{"ok": true}, nil },
	}
}

func flatten(h http.Header) map[string]string {
	out := map[string]string{}
	for k := range h {
		out[k] = h.Get(k)
	}
	return out
}

func lower(h map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[strings.ToLower(k)] = v
	}
	return out
}

func TestFunctionURLHandlerProcessesSignedPush(t *testing.T) {
	eng := orch8test.NewEngine(t)
	task := eng.EnqueueTask("thumb", nil, orch8test.WithQueue("media"))
	h, err := NewFunctionURLHandler(eng.Client(), handlers(), orch8.PushHandlerOptions{Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	body := eng.PushEnvelope(task.ID)
	// Function URLs deliver lower-case header names and may base64 the body.
	resp, err := h(context.Background(), events.LambdaFunctionURLRequest{
		Headers:         lower(flatten(orch8test.SignedPushHeaders("k", body))),
		Body:            base64.StdEncoding.EncodeToString(body),
		IsBase64Encoded: true,
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body %s err %v", resp.StatusCode, resp.Body, err)
	}
	if rec, _ := eng.Task(task.ID); rec.State != orch8test.StateCompleted {
		t.Fatalf("task not completed: %+v", rec)
	}
}

func TestAPIGatewayHandlersRejectBadSignature(t *testing.T) {
	eng := orch8test.NewEngine(t)
	task := eng.EnqueueTask("thumb", nil, orch8test.WithQueue("media"))
	body := eng.PushEnvelope(task.ID)
	headers := flatten(orch8test.SignedPushHeaders("wrong", body))

	v2, err := NewAPIGatewayV2Handler(eng.Client(), handlers(), orch8.PushHandlerOptions{Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := v2(context.Background(), events.APIGatewayV2HTTPRequest{Headers: headers, Body: string(body)})
	v1, _ := NewAPIGatewayProxyHandler(eng.Client(), handlers(), orch8.PushHandlerOptions{Secret: "k"})
	r1, _ := v1(context.Background(), events.APIGatewayProxyRequest{Headers: headers, Body: string(body)})
	if r2.StatusCode != http.StatusUnauthorized || r1.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401s, got %d / %d", r2.StatusCode, r1.StatusCode)
	}
	if _, err := NewFunctionURLHandler(eng.Client(), handlers(), orch8.PushHandlerOptions{}); err == nil {
		t.Fatal("expected error without secret")
	}
}
