package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientRunCredentialContract(t *testing.T) {
	expiry := time.Now().Add(time.Hour).UTC()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer service" {
			t.Error("missing service auth")
		}
		switch r.Method {
		case "POST":
			var request StartRunRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Model == nil || request.ModelCredential == nil || request.ModelCredential.AccessToken != "access" || request.Model.Provider != "openai_chatgpt" {
				t.Error("credential not sent")
			}
			json.NewEncoder(w).Encode(AgentRun{ID: "run", AppID: "helpin"})
		case "PUT":
			if r.URL.Path != "/v1/runs/run/model-credential" || r.URL.Query().Get("app_id") != "helpin" {
				t.Error("incorrect credential scope")
			}
			var request UpdateRunModelCredentialRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
			}
			if request.Credential.AccessToken != "access" {
				t.Error("rotation not sent")
			}
			json.NewEncoder(w).Encode(RunModelCredentialUpdate{RunID: "run", ExpiresAt: &expiry})
		case "DELETE":
			w.WriteHeader(204)
		default:
			t.Error("unexpected method")
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithAppID("helpin"), WithServiceToken("service"))
	if err != nil {
		t.Fatal(err)
	}
	credential := ModelCredential{Type: "oauth", AccessToken: "access", ExpiresAt: &expiry, AccountID: "account", ConnectionID: "connection"}
	run, err := client.StartRun(context.Background(), StartRunRequest{AgentID: "agent", Model: &RunModel{Provider: "openai_chatgpt", Model: "test"}, ModelCredential: &credential})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(run)
	if strings.Contains(string(raw), "access") {
		t.Fatal("secret in run response")
	}
	if _, err = client.UpdateRunModelCredential(context.Background(), run.ID, UpdateRunModelCredentialRequest{Credential: credential}); err != nil {
		t.Fatal(err)
	}
	if err = client.RevokeRunModelCredential(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls: %d", calls)
	}
}
