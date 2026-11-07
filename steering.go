package sdk

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// SteerRunRequest is a durable instruction. DeniedTools is a restriction, never
// a grant. Repeated CommandID values must carry identical content.
type SteerRunRequest struct {
	CommandID        string   `json:"command_id"`
	ExpectedRevision int64    `json:"expected_revision"`
	Content          string   `json:"content"`
	DeniedTools      []string `json:"denied_tools,omitempty"`
	ExternalActorID  string   `json:"external_actor_id"`
}
type RunSteeringCommand struct {
	SteerRunRequest
	RunID     string     `json:"run_id"`
	Revision  int64      `json:"revision"`
	State     string     `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
}

func (c *Client) SteerRun(ctx context.Context, runID string, req SteerRunRequest) (*RunSteeringCommand, error) {
	var out RunSteeringCommand
	err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(runID)+"/steering", c.appQuery(), req, &out)
	return &out, err
}
func (c *Client) RunSteering(ctx context.Context, runID string) ([]RunSteeringCommand, error) {
	var out []RunSteeringCommand
	err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(runID)+"/steering", c.appQuery(), nil, &out)
	return out, err
}

// A2ASteeringExtension is Helpin's versioned acknowledgement contract over
// A2A data parts. It is an extension, not a core A2A pause/steer operation.
const A2ASteeringExtension = "https://helpin.ai/a2a/extensions/steering/v1"
