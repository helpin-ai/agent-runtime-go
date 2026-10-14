package sdk

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenRunEventsPreservesFramesAndIgnoresJSONTimeout(t *testing.T) {
	frames := ": heartbeat\nid: 42\nevent: custom\ndata: untouched\ndata: second line\n\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/runs/run%2Fone/events" || r.URL.Query().Get("app_id") != "rex" || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get(EventProtocolHeader) != "v2" {
			t.Errorf("incorrect stream request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		time.Sleep(30 * time.Millisecond)
		io.WriteString(w, frames)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, WithAppID("rex"), WithServiceToken("secret"), WithEventProtocol("v2"), WithHTTPClient(&http.Client{Timeout: 5 * time.Millisecond}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := client.OpenRunEvents(ctx, "run/one")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != frames {
		t.Fatalf("stream changed or timed out: %q %v", body, err)
	}
}

func TestOpenRunEventsCancellationAndHTTPErrors(t *testing.T) {
	stopped := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "denied") {
			w.WriteHeader(401)
			io.WriteString(w, "denied")
			return
		}
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL)
	_, err := client.OpenRunEvents(context.Background(), "denied")
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != 401 {
		t.Fatalf("status error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	response, err := client.OpenRunEvents(ctx, "running")
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer response.Body.Close()
	cancel()
	if _, err := response.Body.Read(make([]byte, 1)); err == nil {
		t.Fatal("cancelled stream kept reading")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("server stream did not cancel")
	}
}
