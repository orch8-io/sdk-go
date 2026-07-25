package orch8

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGeneratedContractAndTransportFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/transport.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Defaults struct {
			MaxAttempts int `json:"max_attempts"`
		} `json:"defaults"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	foundStream := false
	for _, route := range APIRoutes {
		if route.Path == "/instances/{id}/stream" {
			foundStream = true
			break
		}
	}
	if APIVersion != "1.0.0" || len(APIRoutes) < 100 || !foundStream || fixture.Defaults.MaxAttempts != 3 {
		t.Fatalf("contract mismatch: version=%s routes=%d stream=%v attempts=%d", APIVersion, len(APIRoutes), foundStream, fixture.Defaults.MaxAttempts)
	}
}
