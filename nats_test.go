package sdk

import (
	"testing"
	"time"
)

func TestNATSSubjectHelpers(t *testing.T) {
	if got := AppEventSubject("helpin.stage"); got != "agent-runtime.events.helpin_stage.>" {
		t.Fatalf("unexpected app subject %q", got)
	}
	event := EventEnvelope{
		AppID: "helpin.stage",
		RunID: "run/1",
		Type:  EventAssistantMessageDelta,
	}
	if got := RenderNATSSubject("", event); got != "agent-runtime.events.helpin_stage.run_1.assistant_message_delta" {
		t.Fatalf("unexpected rendered subject %q", got)
	}
	event.Type = EventRunCompleted
	if got := RenderNATSSubject("", event); got != "agent-runtime.events.helpin_stage.run_1.run.completed" {
		t.Fatalf("expected event type dots to remain subject tokens, got %q", got)
	}
}

func TestNATSConsumerConfigDefaults(t *testing.T) {
	cfg := NATSConsumerConfig{AppID: "helpin"}.withDefaults()
	if cfg.Stream != DefaultNATSStreamName {
		t.Fatalf("unexpected stream %q", cfg.Stream)
	}
	if cfg.Subject != "agent-runtime.events.helpin.>" {
		t.Fatalf("unexpected subject %q", cfg.Subject)
	}
	if cfg.Durable != "agent-runtime-projection" {
		t.Fatalf("unexpected durable %q", cfg.Durable)
	}
	if cfg.FetchBatch != 8 || cfg.MaxDeliver != 5 || cfg.MaxAckPending != 64 {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestNATSRetryDelayUsesProgressiveBackoff(t *testing.T) {
	cfg := NATSConsumerConfig{BackOff: []time.Duration{time.Second, 2 * time.Second, 3 * time.Second}}
	for deliveries, want := range map[uint64]time.Duration{
		0: time.Second,
		1: time.Second,
		2: 2 * time.Second,
		3: 3 * time.Second,
		9: 3 * time.Second,
	} {
		if got := cfg.retryDelay(deliveries); got != want {
			t.Fatalf("retryDelay(%d) = %s, want %s", deliveries, got, want)
		}
	}
}
