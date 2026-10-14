package sdk

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// MemoryLearningJob identifies host-owned evidence. Credentials and evidence
// bodies stay in the host; the configured callback resolves them at execution.
type MemoryLearningJob struct {
	ID          string               `json:"id,omitempty"`
	AppID       string               `json:"app_id"`
	BankID      string               `json:"bank_id"`
	Kind        string               `json:"kind"`
	SourceID    string               `json:"source_id"`
	Revision    string               `json:"revision"`
	State       string               `json:"state,omitempty"`
	Attempts    int                  `json:"attempts"`
	RetryAt     *time.Time           `json:"retry_at,omitempty"`
	LastSuccess *time.Time           `json:"last_success,omitempty"`
	Error       string               `json:"error,omitempty"`
	Result      MemoryLearningResult `json:"result"`
	Manual      bool                 `json:"manual,omitempty"`
}

type MemoryLearningResult struct {
	Facts         int    `json:"facts"`
	Processed     int    `json:"processed"`
	Created       int    `json:"created"`
	Updated       int    `json:"updated"`
	Deleted       int    `json:"deleted"`
	InvalidSource bool   `json:"invalid_source,omitempty"`
	Interrupted   bool   `json:"interrupted,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Policy is a short-lived host permission, refreshed while the host is alive.
// Revoking it interrupts background calls without consuming a retry attempt.
type MemoryLearningPolicy struct {
	AppID              string `json:"app_id"`
	AllowExtraction    bool   `json:"allow_extraction"`
	AllowConsolidation bool   `json:"allow_consolidation"`
}

func (c *Client) SubmitMemoryLearning(ctx context.Context, job MemoryLearningJob) (MemoryLearningJob, error) {
	if job.AppID == "" {
		job.AppID = c.appID
	}
	var out MemoryLearningJob
	err := c.doJSON(ctx, http.MethodPost, "/v1/memory/jobs", nil, job, &out)
	return out, err
}

func (c *Client) MemoryLearningJobs(ctx context.Context, bankID string) ([]MemoryLearningJob, error) {
	var out []MemoryLearningJob
	err := c.doJSON(ctx, http.MethodGet, "/v1/memory/jobs", url.Values{"app_id": {c.appID}, "bank_id": {bankID}}, nil, &out)
	return out, err
}

func (c *Client) SetMemoryLearningPolicy(ctx context.Context, policy MemoryLearningPolicy) error {
	if policy.AppID == "" {
		policy.AppID = c.appID
	}
	return c.doJSON(ctx, http.MethodPut, "/v1/memory/learning-policy", nil, policy, nil)
}

func (c *Client) RetryMemoryLearning(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/memory/jobs/"+url.PathEscape(id)+"/retry", url.Values{"app_id": {c.appID}}, nil, nil)
}
