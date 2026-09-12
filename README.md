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
event history, execution details, run tools,
and live Server-Sent Events.

Apps can attach workspace-selected remote MCP servers to an individual run.
The app owns MCP installation and OAuth; the optional `mcpauth` package handles
the reusable discovery, PKCE, registration, exchange, and refresh protocol.
The app supplies workspace/user authorization, browser routes, encrypted state
and refresh-token storage, provider/tool policy, and notifications.

```go
import "github.com/helpin-ai/agent-runtime-go/mcpauth"

oauthClient, err := mcpauth.NewClient(
    installation.EndpointURL,
    mcpauth.WithAllowedHosts("login.provider.example"),
)
configuration, err := oauthClient.Discover(ctx)
registration, err := oauthClient.Register(ctx, configuration.Authorization.RegistrationEndpoint, callbackURL)
authorization, err := oauthClient.NewAuthorizationRequest(configuration, registration.ClientID, callbackURL, installation.Scopes)

// Store mcpauth.HashState(authorization.State), an encrypted verifier, and the
// user/workspace/server binding before redirecting to authorization.URL.
```

At callback, atomically consume that state and call `ExchangeCode`. Call
`Refresh` under an installation lock before runs when needed, persist a rotated
refresh token, and send only the access token to Runtime.

```go
run, err := client.StartRun(ctx, sdk.StartRunRequest{
    AgentID: "agent_123",
    Target: sdk.TargetRef{Type: "workspace", ID: "workspace_123"},
    MCPServers: []sdk.RunMCPServer{{
        ServerID:   "workspace_mcp_456",
        ServerName: "github",
        Transport:  sdk.MCPTransportStreamableHTTP,
        URL:        "https://mcp.example.com/mcp",
        Tools: []sdk.RunMCPTool{
            {Name: "get_issue", Access: sdk.MCPToolAccessRead},
            {Name: "create_issue", Access: sdk.MCPToolAccessWrite},
        },
        Skills: []sdk.SkillRef{{Key: "github_triage"}},
        Credential: &sdk.RunMCPCredential{
            Type:        sdk.MCPCredentialBearerToken,
            AccessToken: shortLivedAccessToken,
            ExpiresAt:   &expiresAt,
        },
    }},
})
```

Credentials are request-only and are not included in the returned run.

Before resuming a run paused for MCP authentication, rotate only its run-scoped
access credential; keep refresh tokens in the host app:

```go
_, err := client.UpdateRunMCPCredential(ctx, run.ID, "workspace_mcp_456", sdk.UpdateRunMCPCredentialRequest{
    Credential: sdk.RunMCPCredential{
        Type: sdk.MCPCredentialBearerToken, AccessToken: accessToken, ExpiresAt: &expiresAt,
    },
})
```

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
