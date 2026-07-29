package sdk

import (
	"encoding/json"
	"testing"
)

func TestParseEventEnvelopeAndUsageData(t *testing.T) {
	payload, _ := json.Marshal(EventEnvelope{
		EventID: "event-1",
		AppID:   "helpin",
		RunID:   "run-1",
		Type:    EventUsageCheckpoint,
		Data: map[string]interface{}{
			"usage_semantic": UsageSemanticCumulative,
			"usage": map[string]interface{}{
				"total_tokens":            float64(12),
				"input_tokens":            float64(4),
				"cached_input_tokens":     float64(1),
				"output_tokens":           float64(2),
				"reasoning_output_tokens": float64(3),
			},
		},
	})
	envelope, err := ParseEventEnvelope(payload)
	if err != nil {
		t.Fatalf("ParseEventEnvelope: %v", err)
	}
	data, ok, err := envelope.UsageCheckpoint()
	if err != nil || !ok {
		t.Fatalf("UsageCheckpoint ok=%v err=%v", ok, err)
	}
	if data.UsageSemantic != UsageSemanticCumulative || data.Usage.TotalTokens != 12 || data.Usage.CachedInputTokens != 1 {
		t.Fatalf("unexpected usage data: %#v", data)
	}
}

func TestParseEventEnvelopeAndCodexAuthStateData(t *testing.T) {
	payload, _ := json.Marshal(EventEnvelope{
		EventID: "event-1",
		AppID:   "helpin",
		RunID:   "run-1",
		Type:    EventCodexAuthStateChanged,
		Data: map[string]interface{}{
			"provider":         "openai",
			"auth_mode":        "chatgpt_device_code",
			"state":            CodexAuthStatePending,
			"verification_url": "https://auth.example/device",
			"user_code":        "ABCD-EFGH",
		},
	})
	envelope, err := ParseEventEnvelope(payload)
	if err != nil {
		t.Fatalf("ParseEventEnvelope: %v", err)
	}
	data, ok, err := envelope.CodexAuthState()
	if err != nil || !ok {
		t.Fatalf("CodexAuthState ok=%v err=%v", ok, err)
	}
	if data.State != CodexAuthStatePending || data.UserCode != "ABCD-EFGH" {
		t.Fatalf("unexpected auth data: %#v", data)
	}
}

func TestParseEventEnvelopeRequiresIdentity(t *testing.T) {
	if _, err := ParseEventEnvelope([]byte(`{"type":"run.completed"}`)); err == nil {
		t.Fatal("expected identity validation error")
	}
}

func TestParseEventEnvelopeV2Metadata(t *testing.T) {
	payload, _ := json.Marshal(EventEnvelope{
		EventID: "event-v2", AppID: "helpin", RunID: "run-1",
		SchemaVersion: EventSchemaVersionV2, SequenceNo: 7,
		TurnID: "turn-1", SegmentID: "message-1", Revision: 3,
		BaseRevision: 2, Type: EventAssistantMessageDelta,
	})
	envelope, err := ParseEventEnvelope(payload)
	if err != nil {
		t.Fatalf("ParseEventEnvelope: %v", err)
	}
	if envelope.SchemaVersion != EventSchemaVersionV2 || envelope.SequenceNo != 7 || envelope.SegmentID != "message-1" || envelope.BaseRevision != 2 {
		t.Fatalf("unexpected v2 metadata: %#v", envelope)
	}
}
