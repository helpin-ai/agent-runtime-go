package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const EventProtocolHeader = "X-Agent-Runtime-Event-Protocol"

type Client struct {
	baseURL       string
	appID         string
	token         string
	eventProtocol string
	httpClient    *http.Client
}

type ClientOption func(*Client)

type HTTPStatusError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("agent runtime %s %s returned %d: %s", e.Method, e.Path, e.StatusCode, strings.TrimSpace(e.Body))
}

func (e *HTTPStatusError) ClientError() bool {
	return e != nil && e.StatusCode >= 400 && e.StatusCode < 500
}

func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func WithServiceToken(token string) ClientOption {
	return func(c *Client) {
		c.token = strings.TrimSpace(token)
	}
}

func WithAppID(appID string) ClientOption {
	return func(c *Client) {
		c.appID = strings.TrimSpace(appID)
	}
}

// WithEventProtocol declares the host projection contract expected for new
// runs. Older clients omit the header and continue to use the runtime's v1
// compatibility behavior.
func WithEventProtocol(protocol string) ClientOption {
	return func(c *Client) {
		c.eventProtocol = strings.ToLower(strings.TrimSpace(protocol))
	}
}

func NewClient(baseURL string, opts ...ClientOption) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("agent runtime base URL is required")
	}
	client := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client, nil
}

func (c *Client) AppID() string {
	if c == nil {
		return ""
	}
	return c.appID
}

func (c *Client) Health(ctx context.Context) (map[string]string, error) {
	var health map[string]string
	if err := c.doJSON(ctx, http.MethodGet, "/healthz", nil, nil, &health); err != nil {
		return nil, err
	}
	return health, nil
}

func (c *Client) GetCapabilities(ctx context.Context) (*Capabilities, error) {
	var capabilities Capabilities
	if err := c.doJSON(ctx, http.MethodGet, "/v1/capabilities", c.appQuery(), nil, &capabilities); err != nil {
		return nil, err
	}
	return &capabilities, nil
}

func (c *Client) GetAppHealth(ctx context.Context) (*AppSummary, error) {
	var summary AppSummary
	if err := c.doJSON(ctx, http.MethodGet, "/v1/app-health", c.appQuery(), nil, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func (c *Client) CreateAgent(ctx context.Context, agent Agent) (*Agent, error) {
	if strings.TrimSpace(agent.AppID) == "" {
		agent.AppID = c.AppID()
	}
	var out Agent
	if err := c.doJSON(ctx, http.MethodPost, "/v1/agents", nil, agent, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StartRun(ctx context.Context, req StartRunRequest) (*AgentRun, error) {
	if strings.TrimSpace(req.AppID) == "" {
		req.AppID = c.AppID()
	}
	var run AgentRun
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs", nil, req, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) GetRun(ctx context.Context, runID string) (*AgentRun, error) {
	var run AgentRun
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID)), c.appQuery(), nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// UpdateRunMCPCredential rotates only the credential for an MCP server that
// was attached when the run started. Host apps should call this before
// resuming a run paused for authentication.
func (c *Client) UpdateRunMCPCredential(
	ctx context.Context,
	runID, serverID string,
	req UpdateRunMCPCredentialRequest,
) (*RunMCPCredentialUpdate, error) {
	path := "/v1/runs/" + url.PathEscape(strings.TrimSpace(runID)) +
		"/mcp-servers/" + url.PathEscape(strings.TrimSpace(serverID)) + "/credential"
	var result RunMCPCredentialUpdate
	if err := c.doJSON(ctx, http.MethodPut, path, c.appQuery(), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListRuns(ctx context.Context) ([]AgentRun, error) {
	var runs []AgentRun
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs", c.appQuery(), nil, &runs); err != nil {
		return nil, err
	}
	return runs, nil
}

func (c *Client) SearchRuns(ctx context.Context, search RunSearchRequest) (*RunPage, error) {
	query := c.appQuery()
	if value := strings.TrimSpace(search.Query); value != "" {
		query.Set("q", value)
	}
	if value := strings.TrimSpace(search.Status); value != "" {
		query.Set("status", value)
	}
	if search.Limit > 0 {
		query.Set("limit", strconv.Itoa(search.Limit))
	}
	if search.Offset > 0 {
		query.Set("offset", strconv.Itoa(search.Offset))
	}
	var page RunPage
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/search", query, nil, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) ResumeRun(ctx context.Context, runID string, req ResumeRunRequest) (*AgentRun, error) {
	var run AgentRun
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/resume", c.appQuery(), req, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) StartCodexDeviceCodeAuth(ctx context.Context, runID string) (*CodexAuthState, error) {
	var state CodexAuthState
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/codex-auth/device-code/start", c.appQuery(), nil, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *Client) CancelCodexDeviceCodeAuth(ctx context.Context, runID string) (*CodexAuthState, error) {
	var state CodexAuthState
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/codex-auth/device-code/cancel", c.appQuery(), nil, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *Client) ApproveRun(ctx context.Context, runID string, externalActorID ...string) (*AgentRun, error) {
	return c.ResumeRun(ctx, runID, ResumeRunRequest{
		Intent:          ResumeIntentApprove,
		ExternalActorID: firstOptionalString(externalActorID),
	})
}

func (c *Client) RequestChanges(ctx context.Context, runID, content string, externalActorID ...string) (*AgentRun, error) {
	return c.ResumeRun(ctx, runID, ResumeRunRequest{
		Intent:          ResumeIntentRequestChanges,
		Content:         content,
		ExternalActorID: firstOptionalString(externalActorID),
	})
}

func (c *Client) CancelRun(ctx context.Context, runID string) (*AgentRun, error) {
	var run AgentRun
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/cancel", c.appQuery(), nil, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *Client) AppendMessage(ctx context.Context, runID string, req AppendMessageRequest) (*AgentRunMessage, error) {
	var message AgentRunMessage
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/messages", c.appQuery(), req, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

func (c *Client) ListMessages(ctx context.Context, runID string) ([]AgentRunMessage, error) {
	var messages []AgentRunMessage
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/messages", c.appQuery(), nil, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// ListV2Events returns durable ordered events after the supplied per-run
// sequence. The v1 client surface remains unchanged for existing consumers.
func (c *Client) ListV2Events(ctx context.Context, runID string, afterSequence int64) (*EventListResponse, error) {
	query := c.appQuery()
	if afterSequence > 0 {
		query.Set("after_sequence", fmt.Sprintf("%d", afterSequence))
	}
	var response EventListResponse
	path := "/v2/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/events"
	if err := c.doJSON(ctx, http.MethodGet, path, query, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// GetV2StreamState returns the authoritative materialized stream snapshot.
func (c *Client) GetV2StreamState(ctx context.Context, runID string) (*StreamStateSnapshot, error) {
	var snapshot StreamStateSnapshot
	path := "/v2/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/stream-state"
	if err := c.doJSON(ctx, http.MethodGet, path, c.appQuery(), nil, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (c *Client) AppendArtifact(ctx context.Context, runID string, req AppendArtifactRequest) (*AgentRunArtifact, error) {
	var artifact AgentRunArtifact
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/artifacts", c.appQuery(), req, &artifact); err != nil {
		return nil, err
	}
	return &artifact, nil
}

func (c *Client) ListArtifacts(ctx context.Context, runID string) ([]AgentRunArtifact, error) {
	var artifacts []AgentRunArtifact
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/artifacts", c.appQuery(), nil, &artifacts); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (c *Client) ListInteractions(ctx context.Context, runID string) ([]AgentRunInteraction, error) {
	var interactions []AgentRunInteraction
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/interactions", c.appQuery(), nil, &interactions); err != nil {
		return nil, err
	}
	return interactions, nil
}

func (c *Client) ListRunEvents(ctx context.Context, runID string) ([]EventEnvelope, error) {
	var events []EventEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/events/history", c.appQuery(), nil, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *Client) GetRunExecution(ctx context.Context, runID string) (*RunExecutionInfo, error) {
	var info RunExecutionInfo
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/execution", c.appQuery(), nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) ListToolCalls(ctx context.Context, runID string) ([]ToolCall, error) {
	var calls []ToolCall
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/tool-calls", c.appQuery(), nil, &calls); err != nil {
		return nil, err
	}
	return calls, nil
}

func (c *Client) ListRunTools(ctx context.Context, runID string) ([]Tool, error) {
	var response struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/tools", c.appQuery(), nil, &response); err != nil {
		return nil, err
	}
	return response.Tools, nil
}

func (c *Client) CallRunTool(ctx context.Context, runID string, req RunToolCallRequest) (*ToolCallResult, error) {
	var result ToolCallResult
	if err := c.doJSON(ctx, http.MethodPost, "/v1/runs/"+url.PathEscape(strings.TrimSpace(runID))+"/tools", c.appQuery(), req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) ListAgents(ctx context.Context) ([]Agent, error) {
	var agents []Agent
	if err := c.doJSON(ctx, http.MethodGet, "/v1/agents", c.appQuery(), nil, &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

func (c *Client) GetAgent(ctx context.Context, agentID string) (*Agent, error) {
	var agent Agent
	if err := c.doJSON(ctx, http.MethodGet, "/v1/agents/"+url.PathEscape(strings.TrimSpace(agentID)), c.appQuery(), nil, &agent); err != nil {
		return nil, err
	}
	return &agent, nil
}

func (c *Client) UpsertAgent(ctx context.Context, agent Agent) (*Agent, error) {
	return c.UpdateAgent(ctx, agent.ID, agent)
}

func (c *Client) UpdateAgent(ctx context.Context, agentID string, agent Agent) (*Agent, error) {
	if strings.TrimSpace(agent.AppID) == "" {
		agent.AppID = c.AppID()
	}
	agent.ID = strings.TrimSpace(agentID)
	var out Agent
	if err := c.doJSON(ctx, http.MethodPut, "/v1/agents/"+url.PathEscape(agent.ID), c.appQuery(), agent, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body interface{}, out interface{}) error {
	if c == nil {
		return fmt.Errorf("agent runtime client is not configured")
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode agent runtime request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.eventProtocol != "" {
		req.Header.Set(EventProtocolHeader, c.eventProtocol)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call agent runtime: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPStatusError{
			Method:     method,
			Path:       path,
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(msg)),
		}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode agent runtime response: %w", err)
	}
	return nil
}

func (c *Client) appQuery() url.Values {
	values := url.Values{}
	if appID := c.AppID(); appID != "" {
		values.Set("app_id", appID)
	}
	return values
}

func firstOptionalString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}
