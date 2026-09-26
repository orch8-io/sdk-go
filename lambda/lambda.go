// Package lambda adapts Orch8 push-dispatch handling to AWS Lambda.
//
// It lives in its own module so github.com/aws/aws-lambda-go stays out of the
// core SDK's dependency graph. Point a push-mode queue's push_url at a Lambda
// Function URL (or API Gateway route) and start the handler:
//
//	client := orch8.NewClient(orch8.ClientConfig{BaseURL: os.Getenv("ORCH8_URL"), TenantID: "acme"})
//	h, err := orch8lambda.NewFunctionURLHandler(client, handlers, orch8.PushHandlerOptions{
//		Secret: os.Getenv("ORCH8_PUSH_SECRET"),
//	})
//	if err != nil { log.Fatal(err) }
//	lambda.Start(h)
//
// Work runs synchronously inside the invocation: the engine waits at most 10s
// per push attempt, so keep handlers short or size MaxClaim accordingly.
package lambda

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	orch8 "github.com/orch8-io/sdk-go"
)

func headerLookup(headers map[string]string) func(string) string {
	return func(name string) string {
		if v, ok := headers[name]; ok {
			return v
		}
		for k, v := range headers {
			if strings.EqualFold(k, name) {
				return v
			}
		}
		return ""
	}
}

func decodeBody(body string, isBase64 bool) ([]byte, error) {
	if !isBase64 {
		return []byte(body), nil
	}
	return base64.StdEncoding.DecodeString(body)
}

func process(ctx context.Context, p *orch8.PushProcessor, headers map[string]string, body string, isBase64 bool) (int, string) {
	raw, err := decodeBody(body, isBase64)
	var res orch8.PushResult
	switch {
	case err != nil:
		res = orch8.PushResult{Status: http.StatusBadRequest, Error: "invalid base64 body"}
	case int64(len(raw)) > p.MaxBodyBytes():
		res = orch8.PushResult{Status: http.StatusRequestEntityTooLarge, Error: "body too large"}
	default:
		res = p.Process(ctx, headerLookup(headers), raw)
	}
	out, _ := json.Marshal(res)
	return res.Status, string(out)
}

var jsonHeaders = map[string]string{"Content-Type": "application/json"}

// NewFunctionURLHandler returns a handler for Lambda Function URL invocations.
func NewFunctionURLHandler(client *orch8.Client, handlers map[string]orch8.HandlerFunc, opts orch8.PushHandlerOptions) (func(context.Context, events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error), error) {
	p, err := newProcessor(client, handlers, opts)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
		status, body := process(ctx, p, req.Headers, req.Body, req.IsBase64Encoded)
		return events.LambdaFunctionURLResponse{StatusCode: status, Headers: jsonHeaders, Body: body}, nil
	}, nil
}

// NewAPIGatewayV2Handler returns a handler for API Gateway HTTP API (payload v2).
func NewAPIGatewayV2Handler(client *orch8.Client, handlers map[string]orch8.HandlerFunc, opts orch8.PushHandlerOptions) (func(context.Context, events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error), error) {
	p, err := newProcessor(client, handlers, opts)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		status, body := process(ctx, p, req.Headers, req.Body, req.IsBase64Encoded)
		return events.APIGatewayV2HTTPResponse{StatusCode: status, Headers: jsonHeaders, Body: body}, nil
	}, nil
}

// NewAPIGatewayProxyHandler returns a handler for API Gateway REST API proxy
// integrations (payload v1).
func NewAPIGatewayProxyHandler(client *orch8.Client, handlers map[string]orch8.HandlerFunc, opts orch8.PushHandlerOptions) (func(context.Context, events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error), error) {
	p, err := newProcessor(client, handlers, opts)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
		status, body := process(ctx, p, req.Headers, req.Body, req.IsBase64Encoded)
		return events.APIGatewayProxyResponse{StatusCode: status, Headers: jsonHeaders, Body: body}, nil
	}, nil
}

func newProcessor(client *orch8.Client, handlers map[string]orch8.HandlerFunc, opts orch8.PushHandlerOptions) (*orch8.PushProcessor, error) {
	opts.Async = false // a Lambda invocation must finish its work before returning
	return orch8.NewPushProcessor(client, handlers, opts)
}
