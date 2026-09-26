package orch8

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestSignPushMatchesEngineScheme(t *testing.T) {
	// HMAC-SHA256("secret", "1000.body"), computed independently with openssl.
	want := "sha256=b758bf26aabb35e5311bc10fd17ab6ad0881e9717e7d5fa7e233f3cea6868b45"
	if got := SignPush("secret", 1000, []byte("body")); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestVerifyPushSignature(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ts := strconv.FormatInt(now.Unix(), 10)
	body := []byte(`{"task_id":"t"}`)
	sig := SignPush("s3cret", now.Unix(), body)
	cases := []struct {
		name            string
		secret, ts, sig string
		body            []byte
		at              time.Time
		want            error
	}{
		{"valid", "s3cret", ts, sig, body, now, nil},
		{"tampered body", "s3cret", ts, sig, []byte(`{"task_id":"u"}`), now, ErrPushInvalidSignature},
		{"wrong secret", "other", ts, sig, body, now, ErrPushInvalidSignature},
		{"missing", "s3cret", "", sig, body, now, ErrPushMissingSignature},
		{"stale", "s3cret", ts, sig, body, now.Add(6 * time.Minute), ErrPushStaleTimestamp},
		{"future", "s3cret", ts, sig, body, now.Add(-6 * time.Minute), ErrPushStaleTimestamp},
		{"no prefix", "s3cret", ts, sig[len("sha256="):], body, now, ErrPushInvalidSignature},
		{"bad hex", "s3cret", ts, "sha256=zz", body, now, ErrPushInvalidSignature},
		{"bad ts", "s3cret", "abc", sig, body, now, ErrPushInvalidSignature},
	}
	for _, tc := range cases {
		err := verifyPushAt(tc.secret, tc.ts, tc.sig, tc.body, 0, tc.at)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v want %v", tc.name, err, tc.want)
		}
	}
}

func TestParsePushEnvelope(t *testing.T) {
	env, err := ParsePushEnvelope([]byte(`{"task_id":"t","instance_id":"i","block_id":"b","handler_name":"h","queue_name":"q","params":{"x":1},"context":{},"attempt":2,"timeout_ms":null}`))
	if err != nil || env.QueueName != "q" || env.Attempt != 2 || string(env.Params) != `{"x":1}` {
		t.Fatal(env, err)
	}
	if _, err := ParsePushEnvelope([]byte(`{"task_id":"t"}`)); err == nil {
		t.Fatal("expected missing handler error")
	}
}

func TestNewPushProcessorRequiresSecretOrOptIn(t *testing.T) {
	c := NewClient(ClientConfig{BaseURL: "http://x"})
	if _, err := NewPushProcessor(c, nil, PushHandlerOptions{}); err == nil {
		t.Fatal("expected error without secret")
	}
	if _, err := NewPushProcessor(c, nil, PushHandlerOptions{AllowUnsigned: true}); err != nil {
		t.Fatal(err)
	}
}
