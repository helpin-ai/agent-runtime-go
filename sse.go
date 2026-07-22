package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// StreamRunEvents consumes the runtime's Server-Sent Events endpoint until
// the context is cancelled, the server closes the stream, or the handler
// returns an error.
func (c *Client) StreamRunEvents(ctx context.Context, runID string, handler EventHandler) error {
	if c == nil {
		return fmt.Errorf("agent runtime client is not configured")
	}
	if handler == nil {
		return fmt.Errorf("event handler is required")
	}
	endpoint := c.baseURL + "/v1/runs/" + url.PathEscape(strings.TrimSpace(runID)) + "/events"
	if query := c.appQuery(); len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call agent runtime: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPStatusError{
			Method:     http.MethodGet,
			Path:       "/v1/runs/{run_id}/events",
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(body)),
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var eventType string
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			eventType = ""
			return nil
		}
		var event EventEnvelope
		if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &event); err != nil {
			return fmt.Errorf("decode agent runtime SSE event: %w", err)
		}
		if strings.TrimSpace(event.Type) == "" {
			event.Type = strings.TrimSpace(eventType)
		}
		if err := validateEventEnvelope(event); err != nil {
			return err
		}
		eventType = ""
		dataLines = dataLines[:0]
		return handler(ctx, event)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			field, value = line, ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			eventType = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read agent runtime SSE stream: %w", err)
	}
	return flush()
}
