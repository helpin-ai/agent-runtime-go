package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientStartRunDefaultsAppIDAndAuth(t *testing.T) {
	var gotAuth string
	var gotEventProtocol string
	var got StartRunRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/runs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotEventProtocol = r.Header.Get(EventProtocolHeader)
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AgentRun{ID: "run-1", AppID: got.AppID, HostRunID: got.HostRunID})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithAppID("helpin"), WithServiceToken("token-1"), WithEventProtocol("v2"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	run, err := client.StartRun(context.Background(), StartRunRequest{
		HostRunID: "host-1",
		AgentID:   "agent-1",
		Target:    TargetRef{Type: "task", ID: "task-1"},
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if got.AppID != "helpin" || got.HostRunID != "host-1" {
		t.Fatalf("unexpected start request: %#v", got)
	}
	if gotAuth != "Bearer token-1" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if gotEventProtocol != "v2" {
		t.Fatalf("event protocol header = %q", gotEventProtocol)
	}
	if run.ID != "run-1" || run.AppID != "helpin" {
		t.Fatalf("unexpected run: %#v", run)
	}
}

func TestClientCurrentRuntimeSurface(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" && r.URL.Query().Get("app_id") != "helpin" && r.URL.Path != "/v1/agents" {
			t.Fatalf("app_id query = %q for %s", r.URL.Query().Get("app_id"), r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /healthz":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "GET /v1/capabilities":
			_, _ = w.Write([]byte(`{"runtime_kinds":["native_sdk"],"providers":[],"store":{"driver":"memory","in_memory":true},"durable":{"enabled":false},"service_auth_enabled":true,"tools":[]}`))
		case "GET /v1/app-health":
			_, _ = w.Write([]byte(`{"app_id":"helpin","components":[{"name":"context","configured":true}]}`))
		case "POST /v1/agents":
			var agent Agent
			if err := json.NewDecoder(r.Body).Decode(&agent); err != nil {
				t.Fatal(err)
			}
			if agent.AppID != "helpin" {
				t.Fatalf("agent app_id = %q", agent.AppID)
			}
			agent.ID = "agent-1"
			_ = json.NewEncoder(w).Encode(agent)
		case "GET /v1/runs/search":
			if r.URL.Query().Get("q") != "repo" || r.URL.Query().Get("status") != RunStatusRunning || r.URL.Query().Get("limit") != "10" || r.URL.Query().Get("offset") != "2" {
				t.Fatalf("unexpected search query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"run-1","app_id":"helpin","agent_id":"agent-1","target":{"type":"repository","id":"repo-1"}}],"total":1,"limit":10,"offset":2}`))
		case "GET /v1/runs/run-1/events/history":
			_, _ = w.Write([]byte(`[{"event_id":"event-1","app_id":"helpin","run_id":"run-1","type":"run.completed"}]`))
		case "GET /v1/runs/run-1/execution":
			_, _ = w.Write([]byte(`{"execution_mode":"durable","state":"running","workflow_id":"workflow-1"}`))
		case "GET /v1/runs/run-1/tools":
			_, _ = w.Write([]byte(`{"tools":[{"name":"workspace.read_file","description":"Read a file","input_schema":{"type":"object"}}]}`))
		case "POST /v1/runs/run-1/tools":
			var request RunToolCallRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.ToolName != "workspace.read_file" {
				t.Fatalf("tool name = %q", request.ToolName)
			}
			_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithAppID("helpin"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if health, err := client.Health(ctx); err != nil || health["status"] != "ok" {
		t.Fatalf("Health() = %#v, %v", health, err)
	}
	if capabilities, err := client.GetCapabilities(ctx); err != nil || capabilities.Store.Driver != "memory" {
		t.Fatalf("GetCapabilities() = %#v, %v", capabilities, err)
	}
	if summary, err := client.GetAppHealth(ctx); err != nil || len(summary.Components) != 1 {
		t.Fatalf("GetAppHealth() = %#v, %v", summary, err)
	}
	if agent, err := client.CreateAgent(ctx, Agent{Name: "Agent"}); err != nil || agent.ID != "agent-1" {
		t.Fatalf("CreateAgent() = %#v, %v", agent, err)
	}
	page, err := client.SearchRuns(ctx, RunSearchRequest{Query: "repo", Status: RunStatusRunning, Limit: 10, Offset: 2})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("SearchRuns() = %#v, %v", page, err)
	}
	if events, err := client.ListRunEvents(ctx, "run-1"); err != nil || len(events) != 1 || events[0].EventID != "event-1" {
		t.Fatalf("ListRunEvents() = %#v, %v", events, err)
	}
	if info, err := client.GetRunExecution(ctx, "run-1"); err != nil || info.WorkflowID != "workflow-1" {
		t.Fatalf("GetRunExecution() = %#v, %v", info, err)
	}
	if tools, err := client.ListRunTools(ctx, "run-1"); err != nil || len(tools) != 1 {
		t.Fatalf("ListRunTools() = %#v, %v", tools, err)
	}
	if result, err := client.CallRunTool(ctx, "run-1", RunToolCallRequest{ToolName: "workspace.read_file"}); err != nil || len(result.Content) != 1 {
		t.Fatalf("CallRunTool() = %#v, %v", result, err)
	}
}

func TestClientResumeRunSendsCorrelationFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ResumeRunRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ResumeID != "resume-1" || request.InteractionID != "interaction-1" || request.Intent != ResumeIntentReply {
			t.Fatalf("unexpected resume request: %#v", request)
		}
		_ = json.NewEncoder(w).Encode(AgentRun{ID: "run-1", AppID: "helpin"})
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, WithAppID("helpin"))
	if _, err := client.ResumeRun(context.Background(), "run-1", ResumeRunRequest{
		Intent:        ResumeIntentReply,
		ResumeID:      "resume-1",
		InteractionID: "interaction-1",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClientStreamsRunEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" || r.URL.Query().Get("app_id") != "helpin" {
			t.Fatalf("unexpected stream request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, ": connected\n\nevent: run.started\ndata: {\"event_id\":\"event-1\",\"app_id\":\"helpin\",\"run_id\":\"run-1\",\"type\":\"run.started\"}\n\nevent: run.completed\ndata: {\"event_id\":\"event-2\",\"app_id\":\"helpin\",\"run_id\":\"run-1\"}\n\n")
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, WithAppID("helpin"))
	var events []EventEnvelope
	err := client.StreamRunEvents(context.Background(), "run-1", func(_ context.Context, event EventEnvelope) error {
		events = append(events, event)
		return nil
	})
	if err != nil || len(events) != 2 || events[1].Type != EventRunCompleted {
		t.Fatalf("StreamRunEvents events=%#v err=%v", events, err)
	}
}

func TestClientListMessagesUsesAppQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/runs/run-1/messages" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("app_id"); got != "helpin" {
			t.Fatalf("app_id query = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]AgentRunMessage{{ID: "msg-1", Role: "assistant"}})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithAppID("helpin"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	messages, err := client.ListMessages(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].ID != "msg-1" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
}

func TestClientCodexDeviceCodeAuthUsesRunSubroutes(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.RequestURI())
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := r.URL.Query().Get("app_id"); got != "helpin" {
			t.Fatalf("app_id query = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CodexAuthState{
			Provider: "openai",
			AuthMode: "chatgpt_device_code",
			State:    CodexAuthStatePending,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithAppID("helpin"), WithServiceToken("token-1"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if state, err := client.StartCodexDeviceCodeAuth(context.Background(), "run-1"); err != nil || state.State != CodexAuthStatePending {
		t.Fatalf("StartCodexDeviceCodeAuth state=%#v err=%v", state, err)
	}
	if state, err := client.CancelCodexDeviceCodeAuth(context.Background(), "run-1"); err != nil || state.State != CodexAuthStatePending {
		t.Fatalf("CancelCodexDeviceCodeAuth state=%#v err=%v", state, err)
	}

	want := []string{
		"POST /v1/runs/run-1/codex-auth/device-code/start?app_id=helpin",
		"POST /v1/runs/run-1/codex-auth/device-code/cancel?app_id=helpin",
	}
	if len(paths) != len(want) {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

func TestClientReturnsHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad app", http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithAppID("helpin"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.GetRun(context.Background(), "run-1")
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected HTTPStatusError, got %T %[1]v", err)
	}
	if statusErr.StatusCode != http.StatusBadRequest || !statusErr.ClientError() {
		t.Fatalf("unexpected status error: %#v", statusErr)
	}
}
