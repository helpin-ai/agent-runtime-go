package sdk

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Run group owners. Host-owned groups contain runs the host started itself and
// keep normal run visibility and callbacks. Work-job groups contain runs the
// runtime created; they are hidden from default listings.
const (
	RunGroupOwnerHost    = "host"
	RunGroupOwnerWorkJob = "work_job"
)

const (
	RunGroupStateActive    = "active"
	RunGroupStatePaused    = "paused"
	RunGroupStateCancelled = "cancelled"
)

// FeatureCapability reports whether an optional runtime feature is usable by
// the authenticated app. A nil capability means the runtime predates the
// feature; Reason explains an explicit unavailable answer.
type FeatureCapability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// RunGroupPolicy bounds the group's aggregate execution. Zero means unbounded.
type RunGroupPolicy struct {
	MaxActiveRuns int `json:"max_active_runs,omitempty"`
}

type RunGroup struct {
	ID          string         `json:"id"`
	AppID       string         `json:"app_id"`
	CreationKey string         `json:"creation_key,omitempty"`
	OwnerKind   string         `json:"owner_kind"`
	ParentRunID string         `json:"parent_run_id,omitempty"`
	State       string         `json:"state"`
	Policy      RunGroupPolicy `json:"policy"`
	// LastCompletionSeq is the highest completion sequence assigned so far.
	LastCompletionSeq int64     `json:"last_completion_seq"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CreateRunGroupRequest creates a host-owned group. Repeating a request with
// the same CreationKey returns the existing group.
type CreateRunGroupRequest struct {
	AppID       string         `json:"app_id"`
	CreationKey string         `json:"creation_key,omitempty"`
	ParentRunID string         `json:"parent_run_id,omitempty"`
	Policy      RunGroupPolicy `json:"policy"`
}

type RunGroupMember struct {
	RunID               string `json:"run_id"`
	GroupID             string `json:"group_id"`
	ParentRunID         string `json:"parent_run_id,omitempty"`
	Status              string `json:"status"`
	ExecutionGeneration int64  `json:"execution_generation"`
	CompletionSeq       int64  `json:"completion_seq,omitempty"`
	Hidden              bool   `json:"hidden,omitempty"`
}

// RunGroupCompletion is persisted in the same transaction as the member's
// first terminal status. Delivery is at-least-once: hosts record the cursor
// and their own advancement in one transaction, idempotent per Seq.
type RunGroupCompletion struct {
	Seq         int64     `json:"seq"`
	RunID       string    `json:"run_id"`
	Status      string    `json:"status"`
	CompletedAt time.Time `json:"completed_at"`
}

type RunGroupCompletionPage struct {
	Items []RunGroupCompletion `json:"items"`
	// NextAfter is the cursor for the next request; it equals the request
	// cursor when no new completions exist.
	NextAfter int64 `json:"next_after"`
}

func (c *Client) CreateRunGroup(ctx context.Context, req CreateRunGroupRequest) (*RunGroup, error) {
	if strings.TrimSpace(req.AppID) == "" {
		req.AppID = c.AppID()
	}
	var group RunGroup
	if err := c.doJSON(ctx, http.MethodPost, "/v1/run-groups", nil, req, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

func (c *Client) GetRunGroup(ctx context.Context, groupID string) (*RunGroup, error) {
	var group RunGroup
	if err := c.doJSON(ctx, http.MethodGet, runGroupPath(groupID, ""), c.appQuery(), nil, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

func (c *Client) ListRunGroupMembers(ctx context.Context, groupID string) ([]RunGroupMember, error) {
	var members []RunGroupMember
	if err := c.doJSON(ctx, http.MethodGet, runGroupPath(groupID, "/members"), c.appQuery(), nil, &members); err != nil {
		return nil, err
	}
	return members, nil
}

// ListRunGroupCompletions returns completions with Seq greater than after, in
// sequence order. limit <= 0 uses the server default.
func (c *Client) ListRunGroupCompletions(ctx context.Context, groupID string, after int64, limit int) (*RunGroupCompletionPage, error) {
	query := c.appQuery()
	query.Set("after", strconv.FormatInt(after, 10))
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var page RunGroupCompletionPage
	if err := c.doJSON(ctx, http.MethodGet, runGroupPath(groupID, "/completions"), query, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// PauseRunGroup stops new admission and pauses active members.
func (c *Client) PauseRunGroup(ctx context.Context, groupID string) (*RunGroup, error) {
	return c.runGroupAction(ctx, groupID, "pause")
}

// ResumeRunGroup reopens admission and resumes members paused by the group.
func (c *Client) ResumeRunGroup(ctx context.Context, groupID string) (*RunGroup, error) {
	return c.runGroupAction(ctx, groupID, "resume")
}

// CancelRunGroup closes admission permanently and cancels every active member.
func (c *Client) CancelRunGroup(ctx context.Context, groupID string) (*RunGroup, error) {
	return c.runGroupAction(ctx, groupID, "cancel")
}

func (c *Client) runGroupAction(ctx context.Context, groupID, action string) (*RunGroup, error) {
	var group RunGroup
	if err := c.doJSON(ctx, http.MethodPost, runGroupPath(groupID, "/"+action), c.appQuery(), nil, &group); err != nil {
		return nil, err
	}
	return &group, nil
}

func runGroupPath(groupID, suffix string) string {
	return "/v1/run-groups/" + url.PathEscape(strings.TrimSpace(groupID)) + suffix
}
