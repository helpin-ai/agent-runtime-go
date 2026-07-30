package mcpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthDiscoveryRegistrationAuthorizationAndExchange(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+baseURL+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": baseURL + "/mcp", "authorization_servers": []string{baseURL}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": baseURL, "authorization_endpoint": baseURL + "/authorize", "token_endpoint": baseURL + "/token",
				"registration_endpoint": baseURL + "/register", "code_challenge_methods_supported": []string{"S256"},
			})
		case "/register":
			_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "app-client", "token_endpoint_auth_method": "none"})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if r.Form.Get("resource") != baseURL+"/mcp" || !pkceVerifierPattern.MatchString(r.Form.Get("code_verifier")) {
				t.Errorf("unexpected token form: %#v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "refresh_token": "refresh", "token_type": "Bearer", "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL

	client, err := NewClient(baseURL+"/mcp", WithInsecureLocalhost(), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := client.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	registration, err := client.Register(context.Background(), configuration.Authorization.RegistrationEndpoint, "https://app.example/callback")
	if err != nil || registration.ClientID != "app-client" {
		t.Fatalf("registration=%#v err=%v", registration, err)
	}
	authorization, err := client.NewAuthorizationRequest(configuration, registration.ClientID, "https://app.example/callback", []string{"read"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(authorization.URL)
	if authorization.State == "" || authorization.Verifier == "" || parsed.Query().Get("code_challenge_method") != "S256" || parsed.Query().Get("resource") != baseURL+"/mcp" {
		t.Fatalf("authorization request=%#v", authorization)
	}
	token, err := client.ExchangeCode(context.Background(), configuration.Authorization.TokenEndpoint, registration.ClientID, "", "none", "code", authorization.Verifier, "https://app.example/callback", baseURL+"/mcp")
	if err != nil || token.AccessToken != "access" || token.ExpiresAt == nil {
		t.Fatalf("token=%#v err=%v", token, err)
	}
	if HashState(authorization.State) == authorization.State || len(HashState(authorization.State)) != 64 {
		t.Fatal("state hash was not a SHA-256 hex digest")
	}
}

func TestClientRejectsUnapprovedDiscoveredHost(t *testing.T) {
	client, err := NewClient("https://mcp.example.com/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ValidateURL("https://auth.other.example/authorize"); err == nil {
		t.Fatal("expected unapproved authorization host rejection")
	}
	client, err = NewClient("https://mcp.example.com/mcp", WithAllowedHosts("auth.other.example"))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ValidateURL("https://auth.other.example/authorize"); err != nil {
		t.Fatalf("explicit authorization host rejected: %v", err)
	}
}

func TestTokenExchangeSupportsClientSecretPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("client_secret") != "secret" {
			t.Errorf("client secret missing: %#v", r.Form)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer"})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, WithInsecureLocalhost(), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	if _, err := client.ExchangeCode(context.Background(), server.URL, "client", "secret", "client_secret_post", "code", verifier, "https://app.example/callback", server.URL); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryRejectsIssuerMismatch(t *testing.T) {
	var baseURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+baseURL+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource/mcp":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": baseURL + "/mcp", "authorization_servers": []string{baseURL}})
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": "https://different.example", "authorization_endpoint": baseURL + "/authorize", "token_endpoint": baseURL + "/token",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL = server.URL
	client, err := NewClient(baseURL+"/mcp", WithInsecureLocalhost(), WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Discover(context.Background()); err == nil {
		t.Fatal("expected issuer mismatch rejection")
	}
}
