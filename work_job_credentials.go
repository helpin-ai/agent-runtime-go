package sdk

import (
	"context"
	"net/http"
	"net/url"
)

// WorkJobCredentialState contains no secrets. Version is an opaque compare-and-swap token.
type WorkJobCredentialState struct {
	Version           string   `json:"version"`
	MCPServerIDs      []string `json:"mcp_server_ids"`
	ModelConnectionID string   `json:"model_connection_id,omitempty"`
}

// UpdateWorkJobCredentialsRequest rotates existing attachments only. It cannot
// add servers, change destinations or grant tools. Active runs are rotated separately.
type UpdateWorkJobCredentialsRequest struct {
	ExpectedVersion string                      `json:"expected_version"`
	MCP             map[string]RunMCPCredential `json:"mcp,omitempty"`
	Model           *ModelCredential            `json:"model,omitempty"`
}

func (c *Client) WorkJobCredentials(ctx context.Context, jobID string) (*WorkJobCredentialState, error) {
	var out WorkJobCredentialState
	err := c.doJSON(ctx, http.MethodGet, "/v1/work-jobs/"+url.PathEscape(jobID)+"/credentials", c.appQuery(), nil, &out)
	return &out, err
}
func (c *Client) UpdateWorkJobCredentials(ctx context.Context, jobID string, req UpdateWorkJobCredentialsRequest) (*WorkJobCredentialState, error) {
	var out WorkJobCredentialState
	err := c.doJSON(ctx, http.MethodPut, "/v1/work-jobs/"+url.PathEscape(jobID)+"/credentials", c.appQuery(), req, &out)
	return &out, err
}
