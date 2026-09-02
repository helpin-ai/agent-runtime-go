package sdk

import (
	"encoding/json"
	"testing"
)

func TestToolProviderMetadataJSONRoundTrip(t *testing.T) {
	original := Tool{
		Name:                 "create_collection",
		Description:          "Create a collection.",
		InputSchema:          json.RawMessage(`{"type":"object"}`),
		Mutating:             true,
		RiskLevel:            RiskLevelRoutine,
		Aliases:              []string{"create_docs_collection"},
		SupportedTargetTypes: []string{"workspace"},
	}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Tool
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RiskLevel != RiskLevelRoutine || len(decoded.Aliases) != 1 || decoded.Aliases[0] != "create_docs_collection" {
		t.Fatalf("metadata did not round-trip: %#v", decoded)
	}
}

func TestToolCallResultStructuredContentJSONRoundTrip(t *testing.T) {
	original := ToolCallResult{StructuredContent: json.RawMessage(`{"collection_id":"col_1"}`)}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ToolCallResult
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded.StructuredContent) != `{"collection_id":"col_1"}` {
		t.Fatalf("unexpected structured content: %s", decoded.StructuredContent)
	}
}
