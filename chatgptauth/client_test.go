package chatgptauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDeviceLoginAndRefresh(t *testing.T) {
	jwt := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`)) + ".signature"
	polls := 0
	exchanges := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			json.NewEncoder(w).Encode(map[string]any{"device_auth_id": "private", "user_code": "CODE", "interval": "2"})
		case "/api/accounts/deviceauth/token":
			polls++
			if polls == 1 {
				w.WriteHeader(403)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"authorization_code": "code", "code_verifier": "verifier"})
		case "/oauth/token":
			r.ParseForm()
			exchanges++
			if exchanges == 1 && (r.Form.Get("code_verifier") != "verifier" || !strings.HasSuffix(r.Form.Get("redirect_uri"), "/deviceauth/callback")) {
				t.Error("invalid exchange")
			}
			if exchanges == 2 && r.Form.Get("refresh_token") != "refresh-1" {
				t.Error("invalid refresh")
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": jwt, "refresh_token": "refresh-" + string(rune('0'+exchanges)), "expires_in": 3600})
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	client, err := NewClient(Config{Issuer: server.URL, AllowLocalHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.StartDeviceLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.PollDeviceLogin(context.Background(), session)
	if err != nil || !result.Pending || polls != 0 {
		t.Fatal("polled before interval")
	}
	session.NextPollAt = time.Time{}
	result, err = client.PollDeviceLogin(context.Background(), session)
	if err != nil || !result.Pending {
		t.Fatalf("pending: %v", err)
	}
	session.NextPollAt = time.Time{}
	result, err = client.PollDeviceLogin(context.Background(), session)
	if err != nil || result.Token == nil {
		t.Fatalf("exchange: %v", err)
	}
	refreshed, err := client.Refresh(context.Background(), *result.Token)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccountID != "account" || refreshed.RefreshToken != "refresh-2" {
		t.Fatal("rotation not preserved")
	}
	session.ExpiresAt = time.Now().Add(-time.Minute)
	if _, err := client.PollDeviceLogin(context.Background(), session); err == nil {
		t.Fatal("expired session accepted")
	}
}
