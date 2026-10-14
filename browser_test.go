package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserMethodsPreserveRuntimeActionsAndScope(t *testing.T) {
	for _, action := range []string{"status", "open", "takeover", "handback", "close"} {
		t.Run(action, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.EscapedPath() != "/v1/browser/agent%2Fone" || r.URL.Query().Get("app_id") != "rex" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("incorrect scoped request: %s", r.URL)
				}
				if action == "status" {
					if r.Method != http.MethodGet {
						t.Error("status must use GET")
					}
				} else {
					var body struct {
						Action string `json:"action"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != http.MethodPost || body.Action != action {
						t.Errorf("action=%+v method=%s err=%v", body, r.Method, err)
					}
				}
				json.NewEncoder(w).Encode(BrowserStatus{Backend: "extension", Active: true, Running: true, Held: action == "takeover", RunID: "run"})
			}))
			defer server.Close()
			client, err := NewClient(server.URL, WithAppID("rex"), WithServiceToken("secret"))
			if err != nil {
				t.Fatal(err)
			}
			methods := map[string]func(context.Context, string) (*BrowserStatus, error){"status": client.GetBrowserStatus, "open": client.OpenBrowser, "takeover": client.TakeOverBrowser, "handback": client.ReleaseBrowser, "close": client.CloseBrowser}
			status, err := methods[action](context.Background(), "agent/one")
			if err != nil || !status.Active || !status.Running || status.RunID != "run" {
				t.Fatalf("status=%+v err=%v", status, err)
			}
		})
	}
}
