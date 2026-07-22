# Agent Runtime Go SDK

Public Go contracts and HTTP client for Agent Runtime hosts.

This package contains only wire contracts, event helpers, host callback DTOs,
and a small `/v1` HTTP client. Product lifecycle policy such as billing,
projection, finalizers, automations, and UI persistence belongs in the host
application.

## Install

```bash
go get github.com/helpin-ai/agent-runtime-go@latest
```

## HTTP client

```go
client, err := sdk.NewClient(
    "https://agent-runtime.internal",
    sdk.WithAppID("host_app"),
    sdk.WithServiceToken("service-token"),
)
if err != nil {
    return err
}

page, err := client.SearchRuns(ctx, sdk.RunSearchRequest{
    Status: sdk.RunStatusRunning,
    Limit:  25,
})
```

The client covers runtime capabilities and app health, agents, runs, persisted
event history, execution details, run tools, Codex device-code authentication,
and live Server-Sent Events.

```go
err = client.StreamRunEvents(ctx, runID, func(ctx context.Context, event sdk.EventEnvelope) error {
    log.Printf("%d %s", event.SequenceNo, event.Type)
    return nil
})
```

## NATS / JetStream events

`NATSConsumer` provides durable pull consumption with explicit
acknowledgements, bounded progressive retries, and the runtime's default
stream/subject conventions.

```go
consumer := sdk.NewNATSConsumer(sdk.NATSConsumerConfig{
    URL:     "nats://nats.internal:4222",
    AppID:   "host_app",
    Durable: "host-app-agent-runtime",
})
err := consumer.Run(ctx, handleEvent)
```

## Test

```bash
go vet ./...
go test ./...
```
