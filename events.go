package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type EventHandler func(ctx context.Context, event EventEnvelope) error

const (
	EventSchemaVersionV2 = "2"

	EventRunQueued    = "run.queued"
	EventRunStarted   = "run.started"
	EventRunResumed   = "run.resumed"
	EventRunPaused    = "run.paused"
	EventRunCompleted = "run.completed"
	EventRunFailed    = "run.failed"
	EventRunCancelled = "run.cancelled"

	EventUsageCheckpoint = "usage.checkpoint"

	EventCodexAuthStateChanged = "codex_auth.state_changed"

	EventAssistantMessageStarted   = "assistant_message_started"
	EventAssistantMessageDelta     = "assistant_message_delta"
	EventAssistantMessageCompleted = "assistant_message_completed"
	EventReasoningMessageStarted   = "reasoning_message_started"
	EventReasoningMessageDelta     = "reasoning_message_delta"
	EventReasoningMessageCompleted = "reasoning_message_completed"

	EventToolCallStarted   = "tool_call_started"
	EventToolCallArgsDelta = "tool_call_args_delta"
	EventToolCallResult    = "tool_call_result"
	EventToolCallFinished  = "tool_call_finished"

	EventPlanUpdated      = "plan_updated"
	EventActivitySnapshot = "activity_snapshot"
	EventActivityDelta    = "activity_delta"

	UsageSemanticCumulative = "cumulative"
	UsageSemanticDelta      = "delta"
)

type Event struct {
	AppID     string                 `json:"app_id"`
	RunID     string                 `json:"run_id"`
	HostRunID string                 `json:"host_run_id,omitempty"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

type EventEnvelope struct {
	EventID       string                 `json:"event_id"`
	SentAt        time.Time              `json:"sent_at"`
	SequenceNo    int64                  `json:"sequence_no"`
	AppID         string                 `json:"app_id"`
	RunID         string                 `json:"run_id"`
	HostRunID     string                 `json:"host_run_id,omitempty"`
	SchemaVersion string                 `json:"schema_version,omitempty"`
	TurnID        string                 `json:"turn_id,omitempty"`
	SegmentID     string                 `json:"segment_id,omitempty"`
	Revision      int64                  `json:"revision,omitempty"`
	BaseRevision  int64                  `json:"base_revision,omitempty"`
	Type          string                 `json:"type"`
	Data          map[string]interface{} `json:"data,omitempty"`
}

// StreamStateSnapshot is the authoritative materialized state of a v2 run
// stream through ThroughSequence. State is intentionally JSON-shaped so hosts
// can project provider-neutral turns without importing runtime internals.
type StreamStateSnapshot struct {
	SchemaVersion   string                 `json:"schema_version"`
	RunID           string                 `json:"run_id"`
	ThroughSequence int64                  `json:"through_sequence"`
	State           map[string]interface{} `json:"state"`
}

// EventListResponse is the replay response returned by the v2 events API.
type EventListResponse struct {
	Events              []EventEnvelope      `json:"events"`
	NextSequenceNo      int64                `json:"next_sequence_no"`
	StreamStateSnapshot *StreamStateSnapshot `json:"stream_state_snapshot,omitempty"`
}

type UsageCheckpointEventData struct {
	Usage         Usage  `json:"usage"`
	UsageSemantic string `json:"usage_semantic,omitempty"`
}

type AssistantMessageEventData struct {
	MessageID string `json:"message_id"`
	Text      string `json:"text,omitempty"`
	Content   string `json:"content,omitempty"`
}

type ReasoningMessageEventData struct {
	MessageID      string `json:"message_id"`
	Text           string `json:"text,omitempty"`
	Content        string `json:"content,omitempty"`
	EncryptedValue string `json:"encrypted_value,omitempty"`
}

type ToolCallEventData struct {
	ToolCallID      string `json:"tool_call_id"`
	ToolName        string `json:"tool_name,omitempty"`
	ToolInput       string `json:"tool_input,omitempty"`
	ParentMessageID string `json:"parent_message_id,omitempty"`
	ResultMessageID string `json:"result_message_id,omitempty"`
	ArgsDelta       string `json:"args_delta,omitempty"`
	ArgsText        string `json:"args_text,omitempty"`
	Content         string `json:"content,omitempty"`
	OutputSummary   string `json:"output_summary,omitempty"`
	DurationMS      int64  `json:"duration_ms,omitempty"`
	Error           string `json:"error,omitempty"`
}

type PlanUpdatedEventData struct {
	Content string                 `json:"content,omitempty"`
	Plan    []RunPlanStep          `json:"plan,omitempty"`
	Note    string                 `json:"note,omitempty"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

type CodexAuthStateEventData struct {
	Provider        string `json:"provider,omitempty"`
	AuthMode        string `json:"auth_mode,omitempty"`
	State           string `json:"state"`
	LoginID         string `json:"login_id,omitempty"`
	AuthURL         string `json:"auth_url,omitempty"`
	VerificationURL string `json:"verification_url,omitempty"`
	UserCode        string `json:"user_code,omitempty"`
	PlanType        string `json:"plan_type,omitempty"`
	Error           string `json:"error,omitempty"`
}

type RunPlanStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

func (e EventEnvelope) DecodeData(out interface{}) error {
	payload, err := json.Marshal(e.Data)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, out)
}

func (e EventEnvelope) UsageCheckpoint() (UsageCheckpointEventData, bool, error) {
	if strings.TrimSpace(e.Type) != EventUsageCheckpoint {
		return UsageCheckpointEventData{}, false, nil
	}
	var data UsageCheckpointEventData
	err := e.DecodeData(&data)
	return data, err == nil, err
}

func (e EventEnvelope) AssistantMessage() (AssistantMessageEventData, bool, error) {
	switch strings.TrimSpace(e.Type) {
	case EventAssistantMessageStarted, EventAssistantMessageDelta, EventAssistantMessageCompleted:
	default:
		return AssistantMessageEventData{}, false, nil
	}
	var data AssistantMessageEventData
	err := e.DecodeData(&data)
	return data, err == nil, err
}

func (e EventEnvelope) ToolCall() (ToolCallEventData, bool, error) {
	switch strings.TrimSpace(e.Type) {
	case EventToolCallStarted, EventToolCallArgsDelta, EventToolCallResult, EventToolCallFinished:
	default:
		return ToolCallEventData{}, false, nil
	}
	var data ToolCallEventData
	err := e.DecodeData(&data)
	return data, err == nil, err
}

func (e EventEnvelope) CodexAuthState() (CodexAuthStateEventData, bool, error) {
	if strings.TrimSpace(e.Type) != EventCodexAuthStateChanged {
		return CodexAuthStateEventData{}, false, nil
	}
	var data CodexAuthStateEventData
	err := e.DecodeData(&data)
	return data, err == nil, err
}

func ParseEventEnvelope(payload []byte) (*EventEnvelope, error) {
	var envelope EventEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	if err := validateEventEnvelope(envelope); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func validateEventEnvelope(envelope EventEnvelope) error {
	if strings.TrimSpace(envelope.AppID) == "" || strings.TrimSpace(envelope.RunID) == "" || strings.TrimSpace(envelope.Type) == "" {
		return fmt.Errorf("event envelope requires app_id, run_id, and type")
	}
	return nil
}
