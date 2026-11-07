package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSteeringAndWorkerCredentialClientContract(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("app_id") != "tenant" || r.Header.Get("Authorization") != "Bearer service" {
			t.Error("missing tenant or service authorization")
		}
		switch r.URL.Path {
		case "/v1/runs/run/steering":
			if r.Method == http.MethodGet {
				json.NewEncoder(w).Encode([]RunSteeringCommand{{State: "applied", Revision: 1}})
				return
			}
			var req SteerRunRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if r.Method != http.MethodPost || req.CommandID != "command" || req.ExpectedRevision != 0 || req.ExternalActorID != "actor" {
				t.Errorf("invalid request: %+v", req)
			}
			json.NewEncoder(w).Encode(RunSteeringCommand{SteerRunRequest: req, Revision: 1, State: "accepted"})
		case "/v1/work-jobs/job/credentials":
			if r.Method == http.MethodPut {
				var req UpdateWorkJobCredentialsRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				if req.ExpectedVersion != "previous" || req.MCP["crm"].AccessToken != "fresh" {
					t.Errorf("missing credential rotation")
				}
			} else if r.Method != http.MethodGet {
				t.Error("invalid credential method")
			}
			json.NewEncoder(w).Encode(WorkJobCredentialState{Version: "next", MCPServerIDs: []string{"crm"}})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithAppID("tenant"), WithServiceToken("service"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	result, err := client.SteerRun(ctx, "run", SteerRunRequest{CommandID: "command", Content: "Research", ExternalActorID: "actor"})
	if err != nil || result.State != "accepted" {
		t.Fatalf("steer: %+v %v", result, err)
	}
	commands, err := client.RunSteering(ctx, "run")
	if err != nil || len(commands) != 1 || commands[0].State != "applied" {
		t.Fatalf("receipts: %+v %v", commands, err)
	}
	if _, err := client.WorkJobCredentials(ctx, "job"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateWorkJobCredentials(ctx, "job", UpdateWorkJobCredentialsRequest{ExpectedVersion: "previous", MCP: map[string]RunMCPCredential{"crm": {Type: "bearer_token", AccessToken: "fresh"}}}); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("calls: %d", calls)
	}
}
