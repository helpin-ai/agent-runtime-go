package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMemoryLearningClientUsesAppScopeAndTypedContract(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("service token missing")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/memory/jobs":
			var j MemoryLearningJob
			json.NewDecoder(r.Body).Decode(&j)
			if j.AppID != "app" || j.BankID != "bank" {
				t.Errorf("scope=%+v", j)
			}
			j.ID = "job"
			j.State = "queued"
			json.NewEncoder(w).Encode(j)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/memory/jobs":
			if r.URL.Query().Get("app_id") != "app" || r.URL.Query().Get("bank_id") != "bank" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode([]MemoryLearningJob{{ID: "job"}})
		case r.Method == http.MethodPut && r.URL.Path == "/v1/memory/learning-policy":
			var p MemoryLearningPolicy
			json.NewDecoder(r.Body).Decode(&p)
			if p.AppID != "app" || !p.AllowExtraction {
				t.Errorf("policy=%+v", p)
			}
			w.WriteHeader(204)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/memory/jobs/job/retry":
			if r.URL.Query().Get("app_id") != "app" {
				t.Error("retry app missing")
			}
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithAppID("app"), WithServiceToken("secret"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	j, err := client.SubmitMemoryLearning(ctx, MemoryLearningJob{BankID: "bank", Kind: "extraction", SourceID: "episode/one", Revision: "one"})
	if err != nil || j.ID != "job" {
		t.Fatalf("job=%+v err=%v", j, err)
	}
	if _, err = client.MemoryLearningJobs(ctx, "bank"); err != nil {
		t.Fatal(err)
	}
	if err = client.SetMemoryLearningPolicy(ctx, MemoryLearningPolicy{AllowExtraction: true}); err != nil {
		t.Fatal(err)
	}
	if err = client.RetryMemoryLearning(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("calls=%d", calls)
	}
}
