package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientStartRunDefaultsAppIDAndAuth(t *testing.T) {
	var gotAuth string
	var got StartRunRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/runs" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AgentRun{ID: "run-1", AppID: got.AppID, HostRunID: got.HostRunID})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithAppID("helpin"), WithServiceToken("token-1"))
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
	if run.ID != "run-1" || run.AppID != "helpin" {
		t.Fatalf("unexpected run: %#v", run)
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
