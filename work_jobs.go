package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Work jobs run one contract over a list of items. The runtime owns the item
// queue: it dispatches bounded batches to worker runs of an existing agent,
// accepts each item result through the worker's submit_item_result tool,
// retries unresolved items in fresh workers and publishes the accepted
// dataset. A worker's final message never completes the job.
//
// Use the work_jobs capability before calling these endpoints. A 404 from
// /v1/work-jobs means only "not available here": it cannot tell an old
// runtime from an app without the capability.

const (
	WorkJobStateQueued    = "queued"
	WorkJobStateRunning   = "running"
	WorkJobStateWaiting   = "waiting"
	WorkJobStatePaused    = "paused"
	WorkJobStateVerifying = "verifying"
	WorkJobStateCompleted = "completed"
	WorkJobStateFailed    = "failed"
	WorkJobStateCancelled = "cancelled"
)

// Waiting reasons. Work continues on other items when a reason is specific
// to one item or worker.
const (
	WorkJobWaitingBudget       = "budget"
	WorkJobWaitingWorkerInput  = "worker_input"  // a worker asked for input or approval
	WorkJobWaitingWorkerAuth   = "worker_auth"   // a worker needs authentication
	WorkJobWaitingNoProgress   = "no_progress"   // consecutive attempts accepted nothing
	WorkJobWaitingExceptions   = "exceptions"    // accepted exceptions exceed the policy
	WorkJobWaitingRetryBackoff = "retry_backoff" // only items in retry backoff remain
	WorkJobWaitingDependencies = "dependencies"  // only items with unmet dependencies remain
	WorkJobWaitingUnavailable  = "unavailable"   // this runtime cannot execute the job now
	// WorkJobWaitingUncertainEffects: a worker that made changes (a mutating
	// tool call) stopped with unresolved items; retrying them could repeat
	// those changes, so they wait until the user resumes the job.
	WorkJobWaitingUncertainEffects = "uncertain_effects"
)

const (
	WorkItemStatePending   = "pending"
	WorkItemStateRunning   = "running"
	WorkItemStateRetryWait = "retry_wait"
	WorkItemStateBlocked   = "blocked"
	WorkItemStateCompleted = "completed"
	WorkItemStateFailed    = "failed"
	WorkItemStateCancelled = "cancelled"
)

// Item outcomes. A completed item is one of these; an unreachable source is
// an exception, never a no_result.
const (
	WorkItemOutcomeCompleted = "completed"
	WorkItemOutcomeNoResult  = "no_result"
	WorkItemOutcomeException = "exception"
)

const (
	WorkAttemptStateOpen      = "open"
	WorkAttemptStateCompleted = "completed" // every assigned item accepted
	WorkAttemptStatePartial   = "partial"   // the worker stopped with unresolved items
	WorkAttemptStateFailed    = "failed"
	WorkAttemptStateCancelled = "cancelled"
)

// WorkJobSpec is the pinned contract every worker receives. Revisions create
// a new SpecRevision; accepted results record the revision they satisfied.
type WorkJobSpec struct {
	// Objective is shown to the user and to every worker.
	Objective string `json:"objective"`
	// Instructions tell a worker how to process one item.
	Instructions string `json:"instructions,omitempty"`
	// ResultSchema is the JSON Schema a completed item's result must satisfy.
	ResultSchema json.RawMessage `json:"result_schema"`
	// NoResultSchema, when set, validates a no_result outcome's result (for
	// example requiring the searches that support it).
	NoResultSchema json.RawMessage `json:"no_result_schema,omitempty"`
	// ExceptionKinds lists the accepted exception kinds; empty allows any.
	ExceptionKinds []string `json:"exception_kinds,omitempty"`
	// RetryableExceptionKinds are retried in a fresh worker until the item's
	// attempts are exhausted, then accepted as exceptions.
	RetryableExceptionKinds []string `json:"retryable_exception_kinds,omitempty"`
	// BatchSize is the number of items per worker attempt (1-50, default 10).
	BatchSize int `json:"batch_size,omitempty"`
	// MaxAttemptsPerItem bounds how often one item is assigned (default 3).
	MaxAttemptsPerItem int `json:"max_attempts_per_item,omitempty"`
	// MaxNoProgressAttempts consecutive attempts without an accepted item put
	// the job in waiting/no_progress (default 3).
	MaxNoProgressAttempts int `json:"max_no_progress_attempts,omitempty"`
	// MaxExceptionRatio is the share of items that may end as accepted
	// exceptions before the job waits for a decision (default 0.1).
	MaxExceptionRatio *float64 `json:"max_exception_ratio,omitempty"`
	// MaxConcurrency is the number of workers running at once (default 1).
	MaxConcurrency int `json:"max_concurrency,omitempty"`
	// SerializeBrowser runs at most one browser-using worker at a time; set
	// when workers share one local browser profile.
	SerializeBrowser bool          `json:"serialize_browser,omitempty"`
	Budget           WorkJobBudget `json:"budget,omitempty"`
}

// WorkJobBudget bounds reported model tokens. It is not a monetary cap:
// providers may charge for work they do not report.
type WorkJobBudget struct {
	// MaxTokens bounds input+output tokens over all workers; 0 is unbounded.
	MaxTokens int64 `json:"max_tokens,omitempty"`
	// ReserveTokensPerAttempt is held before a worker starts and released
	// when its actual usage is applied (default 200000 when MaxTokens is set).
	ReserveTokensPerAttempt int64 `json:"reserve_tokens_per_attempt,omitempty"`
}

// WorkJobWorker is the run template for every worker. Its fields follow
// StartRunRequest; the runtime adds the job contract, the assigned items and
// the submit_item_result tool. Credentials are stored encrypted with the job
// and copied to each worker run.
type WorkJobWorker struct {
	AgentID         string                 `json:"agent_id"`
	Target          TargetRef              `json:"target"`
	Model           *RunModel              `json:"model,omitempty"`
	ModelCredential *ModelCredential       `json:"model_credential,omitempty"`
	AllowedTools    []string               `json:"allowed_tools,omitempty"`
	ExternalActorID string                 `json:"external_actor_id,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	MCPServers      []RunMCPServer         `json:"mcp_servers,omitempty"`
}

type WorkItemInput struct {
	// ID is stable and unique within the job.
	ID    string          `json:"id"`
	Input json.RawMessage `json:"input"`
	// DependsOn lists item IDs that must complete first.
	DependsOn []string `json:"depends_on,omitempty"`
}

// CreateWorkJobRequest creates a job. Repeating a request with the same
// IdempotencyKey returns the existing job.
type CreateWorkJobRequest struct {
	AppID          string          `json:"app_id"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	Spec           WorkJobSpec     `json:"spec"`
	Worker         WorkJobWorker   `json:"worker"`
	Items          []WorkItemInput `json:"items"`
	// ParentRunID records the conversational run that created the job. The
	// job does not end with that run.
	ParentRunID string `json:"parent_run_id,omitempty"`
}

type WorkJobCounts struct {
	Total      int `json:"total"`
	Pending    int `json:"pending"`
	Running    int `json:"running"`
	RetryWait  int `json:"retry_wait"`
	Blocked    int `json:"blocked"`
	Completed  int `json:"completed"`
	Found      int `json:"found"`
	NoResult   int `json:"no_result"`
	Exceptions int `json:"exceptions"`
	Failed     int `json:"failed"`
	Cancelled  int `json:"cancelled"`
}

// WorkJobUsage is applied from each worker's persisted cumulative usage.
type WorkJobUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	ReservedTokens    int64 `json:"reserved_tokens"`
}

type WorkJob struct {
	ID            string `json:"id"`
	AppID         string `json:"app_id"`
	GroupID       string `json:"group_id"`
	ParentRunID   string `json:"parent_run_id,omitempty"`
	State         string `json:"state"`
	WaitingReason string `json:"waiting_reason,omitempty"`
	// BlockedRunID is the worker run waiting for input, approval or auth.
	BlockedRunID string        `json:"blocked_run_id,omitempty"`
	Error        string        `json:"error,omitempty"`
	SpecRevision int           `json:"spec_revision"`
	Spec         WorkJobSpec   `json:"spec"`
	Counts       WorkJobCounts `json:"counts"`
	Usage        WorkJobUsage  `json:"usage"`
	ActiveRuns   []string      `json:"active_runs"`
	// DatasetRevision increases each time the accepted dataset is published.
	DatasetRevision int        `json:"dataset_revision"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type WorkItemException struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type WorkItem struct {
	ID           string             `json:"id"`
	JobID        string             `json:"job_id"`
	State        string             `json:"state"`
	Input        json.RawMessage    `json:"input"`
	DependsOn    []string           `json:"depends_on,omitempty"`
	Attempts     int                `json:"attempts"`
	Outcome      string             `json:"outcome,omitempty"`
	Result       json.RawMessage    `json:"result,omitempty"`
	Exception    *WorkItemException `json:"exception,omitempty"`
	SpecRevision int                `json:"spec_revision,omitempty"`
	AttemptID    string             `json:"attempt_id,omitempty"`
	RunID        string             `json:"run_id,omitempty"`
	LastError    string             `json:"last_error,omitempty"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type WorkItemPage struct {
	Items []WorkItem `json:"items"`
	// NextAfter is the item ID to pass as after for the next page; empty at
	// the end.
	NextAfter string `json:"next_after,omitempty"`
}

type WorkAttempt struct {
	ID         string   `json:"id"`
	JobID      string   `json:"job_id"`
	RunID      string   `json:"run_id,omitempty"`
	State      string   `json:"state"`
	ItemIDs    []string `json:"item_ids"`
	Accepted   int      `json:"accepted"`
	Generation int64    `json:"generation"`
	// SpecRevision is the job specification the batch was given.
	SpecRevision int    `json:"spec_revision"`
	Outcome      string `json:"outcome,omitempty"`
	Tokens       int64  `json:"tokens"`
	// UsagePending reports a finished attempt whose worker usage is not yet
	// known; its reservation still counts against the budget.
	UsagePending bool       `json:"usage_pending,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
}

type WorkJobEvent struct {
	Seq       int64                  `json:"seq"`
	JobID     string                 `json:"job_id"`
	Type      string                 `json:"type"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type WorkJobEventPage struct {
	Items     []WorkJobEvent `json:"items"`
	NextAfter int64          `json:"next_after"`
}

// WorkJobDataset is the published, versioned set of accepted item results.
type WorkJobDataset struct {
	JobID    string            `json:"job_id"`
	Revision int               `json:"revision"`
	Digest   string            `json:"digest"`
	Items    []WorkDatasetItem `json:"items"`
	Created  time.Time         `json:"created_at"`
}

type WorkDatasetItem struct {
	ID           string             `json:"id"`
	Outcome      string             `json:"outcome"`
	Result       json.RawMessage    `json:"result,omitempty"`
	Exception    *WorkItemException `json:"exception,omitempty"`
	SpecRevision int                `json:"spec_revision"`
}

// ReviseWorkJobRequest replaces the job specification. ExpectedRevision must
// match the current SpecRevision. Items whose accepted results no longer
// validate are requeued when Revalidate is set.
type ReviseWorkJobRequest struct {
	ExpectedRevision int         `json:"expected_revision"`
	Spec             WorkJobSpec `json:"spec"`
	Revalidate       bool        `json:"revalidate,omitempty"`
}

// AddWorkItemsRequest appends items to a job that has not completed.
type AddWorkItemsRequest struct {
	Items []WorkItemInput `json:"items"`
}

func (c *Client) CreateWorkJob(ctx context.Context, req CreateWorkJobRequest) (*WorkJob, error) {
	if strings.TrimSpace(req.AppID) == "" {
		req.AppID = c.AppID()
	}
	var job WorkJob
	if err := c.doJSON(ctx, http.MethodPost, "/v1/work-jobs", nil, req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) GetWorkJob(ctx context.Context, jobID string) (*WorkJob, error) {
	var job WorkJob
	if err := c.doJSON(ctx, http.MethodGet, workJobPath(jobID, ""), c.appQuery(), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) ListWorkJobs(ctx context.Context) ([]WorkJob, error) {
	var jobs []WorkJob
	if err := c.doJSON(ctx, http.MethodGet, "/v1/work-jobs", c.appQuery(), nil, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

// ListWorkItems pages items in ID order. state filters when set.
func (c *Client) ListWorkItems(ctx context.Context, jobID, state, after string, limit int) (*WorkItemPage, error) {
	query := c.appQuery()
	if state != "" {
		query.Set("state", state)
	}
	if after != "" {
		query.Set("after", after)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var page WorkItemPage
	if err := c.doJSON(ctx, http.MethodGet, workJobPath(jobID, "/items"), query, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) AddWorkItems(ctx context.Context, jobID string, req AddWorkItemsRequest) (*WorkJob, error) {
	var job WorkJob
	if err := c.doJSON(ctx, http.MethodPost, workJobPath(jobID, "/items"), c.appQuery(), req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) ListWorkAttempts(ctx context.Context, jobID string) ([]WorkAttempt, error) {
	var attempts []WorkAttempt
	if err := c.doJSON(ctx, http.MethodGet, workJobPath(jobID, "/attempts"), c.appQuery(), nil, &attempts); err != nil {
		return nil, err
	}
	return attempts, nil
}

// ListWorkJobEvents returns job events with Seq greater than after.
func (c *Client) ListWorkJobEvents(ctx context.Context, jobID string, after int64, limit int) (*WorkJobEventPage, error) {
	query := c.appQuery()
	query.Set("after", strconv.FormatInt(after, 10))
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var page WorkJobEventPage
	if err := c.doJSON(ctx, http.MethodGet, workJobPath(jobID, "/events"), query, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// GetWorkJobDataset returns the latest published dataset.
func (c *Client) GetWorkJobDataset(ctx context.Context, jobID string) (*WorkJobDataset, error) {
	var dataset WorkJobDataset
	if err := c.doJSON(ctx, http.MethodGet, workJobPath(jobID, "/dataset"), c.appQuery(), nil, &dataset); err != nil {
		return nil, err
	}
	return &dataset, nil
}

func (c *Client) ReviseWorkJob(ctx context.Context, jobID string, req ReviseWorkJobRequest) (*WorkJob, error) {
	var job WorkJob
	if err := c.doJSON(ctx, http.MethodPost, workJobPath(jobID, "/revise"), c.appQuery(), req, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// PauseWorkJob stops new dispatch; running workers finish their batch.
func (c *Client) PauseWorkJob(ctx context.Context, jobID string) (*WorkJob, error) {
	return c.workJobAction(ctx, jobID, "pause")
}

// ResumeWorkJob resumes dispatch, and clears a waiting state the user has
// decided (no progress, exceptions over policy, budget after a raise).
func (c *Client) ResumeWorkJob(ctx context.Context, jobID string) (*WorkJob, error) {
	return c.workJobAction(ctx, jobID, "resume")
}

// CancelWorkJob cancels every active worker. Accepted results are kept.
func (c *Client) CancelWorkJob(ctx context.Context, jobID string) (*WorkJob, error) {
	return c.workJobAction(ctx, jobID, "cancel")
}

func (c *Client) workJobAction(ctx context.Context, jobID, action string) (*WorkJob, error) {
	var job WorkJob
	if err := c.doJSON(ctx, http.MethodPost, workJobPath(jobID, "/"+action), c.appQuery(), nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func workJobPath(jobID, suffix string) string {
	return "/v1/work-jobs/" + url.PathEscape(strings.TrimSpace(jobID)) + suffix
}
