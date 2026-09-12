// Package chatgptauth implements app-side ChatGPT device authentication.
// Apps own durable encrypted storage, user authorization, and refresh locking.
// Protocol reference: openai/codex, Apache-2.0, commit 53c542d944c705f3a66780a19223223bee57cbb6.
package chatgptauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

type Config struct {
	Issuer, ClientID string
	HTTPClient       *http.Client
	AllowLocalHTTP   bool
}
type Client struct {
	issuer, clientID string
	http             *http.Client
}
type DeviceSession struct {
	VerificationURL string    `json:"verification_url"`
	UserCode        string    `json:"user_code"`
	DeviceAuthID    string    `json:"device_auth_id"`
	IntervalSeconds int       `json:"interval_seconds"`
	ExpiresAt       time.Time `json:"expires_at"`
	NextPollAt      time.Time `json:"next_poll_at"`
}

func (DeviceSession) String() string   { return "[private device session]" }
func (DeviceSession) GoString() string { return "[private device session]" }

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AccountID    string    `json:"account_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func (Token) String() string   { return "[redacted ChatGPT token]" }
func (Token) GoString() string { return "[redacted ChatGPT token]" }

type AuthError struct {
	Code   string
	Status int
}

func (e *AuthError) Error() string { return "ChatGPT authentication: " + e.Code }

type PollResult struct {
	Pending bool
	Token   *Token
}

func NewClient(cfg Config) (*Client, error) {
	issuer := strings.TrimRight(cfg.Issuer, "/")
	if issuer == "" {
		issuer = "https://auth.openai.com"
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(cfg.AllowLocalHTTP && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, errors.New("invalid ChatGPT auth issuer")
	}
	client := http.Client{Timeout: 20 * time.Second}
	if cfg.HTTPClient != nil {
		client = *cfg.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	id := cfg.ClientID
	if id == "" {
		id = DefaultClientID
	}
	return &Client{issuer: issuer, clientID: id, http: &client}, nil
}

func (c *Client) request(ctx context.Context, path, contentType, body string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.issuer+path, strings.NewReader(body))
	if err != nil {
		return nil, 0, &AuthError{Code: "request_failed"}
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, &AuthError{Code: "request_failed"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return nil, resp.StatusCode, &AuthError{Code: "invalid_response"}
	}
	return raw, resp.StatusCode, nil
}

func (c *Client) StartDeviceLogin(ctx context.Context) (*DeviceSession, error) {
	body, _ := json.Marshal(map[string]string{"client_id": c.clientID})
	raw, status, err := c.request(ctx, "/api/accounts/deviceauth/usercode", "application/json", string(body))
	if err != nil {
		return nil, err
	}
	if status != 200 {
		code := "login_unavailable"
		if status == 404 {
			code = "device_login_disabled"
		}
		return nil, &AuthError{Code: code, Status: status}
	}
	var response struct {
		DeviceAuthID   string          `json:"device_auth_id"`
		UserCode       string          `json:"user_code"`
		LegacyUserCode string          `json:"usercode"`
		Interval       json.RawMessage `json:"interval"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return nil, &AuthError{Code: "invalid_response"}
	}
	if response.UserCode == "" {
		response.UserCode = response.LegacyUserCode
	}
	if response.DeviceAuthID == "" || response.UserCode == "" {
		return nil, &AuthError{Code: "invalid_response"}
	}
	interval, _ := strconv.Atoi(strings.Trim(string(response.Interval), "\" "))
	if interval < 1 {
		interval = 5
	}
	if interval > 60 {
		interval = 60
	}
	now := time.Now().UTC()
	return &DeviceSession{VerificationURL: c.issuer + "/codex/device", UserCode: response.UserCode, DeviceAuthID: response.DeviceAuthID, IntervalSeconds: interval, ExpiresAt: now.Add(15 * time.Minute), NextPollAt: now.Add(time.Duration(interval) * time.Second)}, nil
}

// PollDeviceLogin makes at most one poll and exchanges an approved code. Persist
// the updated session after every call and serialize calls for each login.
func (c *Client) PollDeviceLogin(ctx context.Context, s *DeviceSession) (*PollResult, error) {
	if s == nil || s.DeviceAuthID == "" || s.UserCode == "" {
		return nil, &AuthError{Code: "invalid_session"}
	}
	now := time.Now().UTC()
	if !s.ExpiresAt.After(now) {
		return nil, &AuthError{Code: "expired"}
	}
	if s.NextPollAt.After(now) {
		return &PollResult{Pending: true}, nil
	}
	interval := s.IntervalSeconds
	if interval < 1 {
		interval = 5
	}
	if interval > 60 {
		interval = 60
	}
	s.NextPollAt = now.Add(time.Duration(interval) * time.Second)
	body, _ := json.Marshal(map[string]string{"device_auth_id": s.DeviceAuthID, "user_code": s.UserCode})
	raw, status, err := c.request(ctx, "/api/accounts/deviceauth/token", "application/json", string(body))
	if err != nil {
		return nil, err
	}
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &failure) == nil && (failure.Error == "access_denied" || failure.Error == "expired_token") {
		return nil, &AuthError{Code: "authorization_failed", Status: status}
	}
	if status == 403 || status == 404 {
		return &PollResult{Pending: true}, nil
	}
	if status == 429 {
		s.IntervalSeconds = min(60, interval+5)
		s.NextPollAt = now.Add(time.Duration(s.IntervalSeconds) * time.Second)
		return &PollResult{Pending: true}, nil
	}
	if status != 200 {
		return nil, &AuthError{Code: "authorization_failed", Status: status}
	}
	var code struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if json.Unmarshal(raw, &code) != nil || code.AuthorizationCode == "" || code.CodeVerifier == "" {
		return nil, &AuthError{Code: "invalid_response"}
	}
	token, err := c.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code.AuthorizationCode}, "code_verifier": {code.CodeVerifier}, "redirect_uri": {c.issuer + "/deviceauth/callback"}}, "")
	if err != nil {
		return nil, err
	}
	return &PollResult{Token: token}, nil
}

func (c *Client) Refresh(ctx context.Context, old Token) (*Token, error) {
	if old.RefreshToken == "" {
		return nil, &AuthError{Code: "reconnect_required"}
	}
	next, err := c.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old.RefreshToken}}, old.RefreshToken)
	if err != nil {
		return nil, err
	}
	if old.AccountID != "" && old.AccountID != next.AccountID {
		return nil, &AuthError{Code: "account_changed"}
	}
	return next, nil
}

func (c *Client) exchange(ctx context.Context, form url.Values, previousRefresh string) (*Token, error) {
	form.Set("client_id", c.clientID)
	raw, status, err := c.request(ctx, "/oauth/token", "application/x-www-form-urlencoded", form.Encode())
	if err != nil {
		return nil, err
	}
	if status != 200 {
		code := "refresh_unavailable"
		if status == 400 || status == 401 || status == 403 {
			code = "reconnect_required"
		}
		return nil, &AuthError{Code: code, Status: status}
	}
	var response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(raw, &response) != nil || response.AccessToken == "" || response.ExpiresIn <= 0 || response.ExpiresIn > 366*86400 {
		return nil, &AuthError{Code: "invalid_response"}
	}
	if response.RefreshToken == "" {
		response.RefreshToken = previousRefresh
	}
	if response.RefreshToken == "" {
		return nil, &AuthError{Code: "invalid_response"}
	}
	account, err := AccountID(response.AccessToken)
	if err != nil {
		return nil, err
	}
	return &Token{AccessToken: response.AccessToken, RefreshToken: response.RefreshToken, AccountID: account, ExpiresAt: time.Now().UTC().Add(time.Duration(response.ExpiresIn) * time.Second)}, nil
}

// AccountID reads routing metadata from a token returned by the trusted token
// endpoint. It does not verify a JWT and must not authorize an app user.
func AccountID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", &AuthError{Code: "invalid_token"}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", &AuthError{Code: "invalid_token"}
	}
	var claims struct {
		Auth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(raw, &claims) != nil || claims.Auth.AccountID == "" {
		return "", &AuthError{Code: "missing_account"}
	}
	return claims.Auth.AccountID, nil
}
