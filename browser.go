package sdk

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type BrowserStatus struct {
	Backend string `json:"backend"`
	Running bool   `json:"running"`
	Active  bool   `json:"active"`
	Held    bool   `json:"held"`
	RunID   string `json:"run_id,omitempty"`
}

func (c *Client) GetBrowserStatus(ctx context.Context, agentID string) (*BrowserStatus, error) {
	return c.browserControl(ctx, agentID, "status")
}
func (c *Client) OpenBrowser(ctx context.Context, agentID string) (*BrowserStatus, error) {
	return c.browserControl(ctx, agentID, "open")
}
func (c *Client) TakeOverBrowser(ctx context.Context, agentID string) (*BrowserStatus, error) {
	return c.browserControl(ctx, agentID, "takeover")
}
func (c *Client) ReleaseBrowser(ctx context.Context, agentID string) (*BrowserStatus, error) {
	return c.browserControl(ctx, agentID, "handback")
}
func (c *Client) CloseBrowser(ctx context.Context, agentID string) (*BrowserStatus, error) {
	return c.browserControl(ctx, agentID, "close")
}
func (c *Client) browserControl(ctx context.Context, agentID, action string) (*BrowserStatus, error) {
	if strings.TrimSpace(agentID) == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	method := http.MethodPost
	var body any = struct {
		Action string `json:"action"`
	}{action}
	if action == "status" {
		method, body = http.MethodGet, nil
	}
	var out BrowserStatus
	if err := c.doJSON(ctx, method, "/v1/browser/"+url.PathEscape(agentID), c.appQuery(), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
