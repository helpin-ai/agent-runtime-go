package sdk

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RunModel pins an execution route without modifying the reusable agent.
type RunModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// ModelCredential is request-only. Send from your backend, never place it in
// run metadata or transcripts. Refresh tokens remain in the host application.
type ModelCredential struct {
	Type         string     `json:"type"`
	APIKey       string     `json:"api_key,omitempty"`
	AccessToken  string     `json:"access_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	ConnectionID string     `json:"connection_id,omitempty"`
	AccountID    string     `json:"account_id,omitempty"`
}

func (ModelCredential) String() string   { return "[redacted model credential]" }
func (ModelCredential) GoString() string { return "[redacted model credential]" }

type ModelCredentialRefreshRequest struct {
	CredentialFingerprint string `json:"credential_fingerprint,omitempty"`
	AppID                 string `json:"app_id"`
	RunID                 string `json:"run_id"`
	HostRunID             string `json:"host_run_id,omitempty"`
	ConnectionID          string `json:"connection_id"`
	Provider              string `json:"provider"`
	AccountID             string `json:"account_id,omitempty"`
	Reason                string `json:"reason"`
}

type UpdateRunModelCredentialRequest struct {
	Credential ModelCredential `json:"credential"`
}

type RunModelCredentialUpdate struct {
	RunID     string     `json:"run_id"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

func (c *Client) UpdateRunModelCredential(ctx context.Context, runID string, req UpdateRunModelCredentialRequest) (*RunModelCredentialUpdate, error) {
	var out RunModelCredentialUpdate
	err := c.doJSON(ctx, http.MethodPut, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/model-credential", c.appQuery(), req, &out)
	return &out, err
}

// RevokeRunModelCredential prevents further model requests on this run.
func (c *Client) RevokeRunModelCredential(ctx context.Context, runID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/model-credential", c.appQuery(), nil, nil)
}
