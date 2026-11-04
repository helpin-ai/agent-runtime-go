package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunGroupClientRoutes(t *testing.T) {
	var created CreateRunGroupRequest
	var started StartRunRequest
	seen := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.Method+" "+r.URL.Path] = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/run-groups":
			_ = json.NewDecoder(r.Body).Decode(&created)
			_ = json.NewEncoder(w).Encode(RunGroup{ID: "grp-1", AppID: created.AppID, OwnerKind: RunGroupOwnerHost, State: RunGroupStateActive})
		case "POST /v1/runs":
			_ = json.NewDecoder(r.Body).Decode(&started)
			_ = json.NewEncoder(w).Encode(AgentRun{ID: "run-1"})
		case "GET /v1/run-groups/grp-1/completions":
			_ = json.NewEncoder(w).Encode(RunGroupCompletionPage{Items: []RunGroupCompletion{{Seq: 3, RunID: "run-1", Status: RunStatusCompleted}}, NextAfter: 3})
		case "GET /v1/run-groups/grp-1/members":
			_ = json.NewEncoder(w).Encode([]RunGroupMember{{RunID: "run-1", GroupID: "grp-1", ExecutionGeneration: 1}})
		case "POST /v1/run-groups/grp-1/cancel":
			_ = json.NewEncoder(w).Encode(RunGroup{ID: "grp-1", State: RunGroupStateCancelled})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, WithServiceToken("token"), WithAppID("app-1"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	group, err := client.CreateRunGroup(ctx, CreateRunGroupRequest{CreationKey: "plan-7", Policy: RunGroupPolicy{MaxActiveRuns: 2}})
	if err != nil || group.ID != "grp-1" {
		t.Fatalf("create group = %#v, %v", group, err)
	}
	if created.AppID != "app-1" || created.CreationKey != "plan-7" || created.Policy.MaxActiveRuns != 2 {
		t.Fatalf("create request = %#v", created)
	}
	if _, err := client.StartRun(ctx, StartRunRequest{AgentID: "agent-1", GroupID: "grp-1", ParentRunID: "run-0"}); err != nil {
		t.Fatal(err)
	}
	if started.GroupID != "grp-1" || started.ParentRunID != "run-0" {
		t.Fatalf("start request = %#v", started)
	}
	page, err := client.ListRunGroupCompletions(ctx, "grp-1", 2, 50)
	if err != nil || page.NextAfter != 3 || len(page.Items) != 1 {
		t.Fatalf("completions = %#v, %v", page, err)
	}
	if got := seen["GET /v1/run-groups/grp-1/completions"]; got != "after=2&app_id=app-1&limit=50" {
		t.Fatalf("completion query = %q", got)
	}
	if members, err := client.ListRunGroupMembers(ctx, "grp-1"); err != nil || len(members) != 1 {
		t.Fatalf("members = %#v, %v", members, err)
	}
	if group, err := client.CancelRunGroup(ctx, "grp-1"); err != nil || group.State != RunGroupStateCancelled {
		t.Fatalf("cancel = %#v, %v", group, err)
	}
}

func TestStartRunOmitsGroupFieldsForLegacyCallers(t *testing.T) {
	payload, err := json.Marshal(StartRunRequest{AppID: "app-1", AgentID: "agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"group_id", "parent_run_id"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("legacy start request includes %q: %s", key, payload)
		}
	}
}

func TestCapabilitiesWithoutRunGroupsDecodesAsUnknown(t *testing.T) {
	var caps Capabilities
	if err := json.Unmarshal([]byte(`{"runtime_kinds":["native"],"providers":[],"store":{},"durable":{},"service_auth_enabled":true,"tools":[],"run_mcp":{"supported":true,"transports":[]}}`), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.RunGroups != nil {
		t.Fatalf("old runtime capabilities must leave RunGroups nil, got %#v", caps.RunGroups)
	}
}

func TestRunGroupNotFoundIsHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	client, err := NewClient(server.URL, WithAppID("app-1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetRunGroup(context.Background(), "missing")
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 HTTPStatusError, got %v", err)
	}
}
