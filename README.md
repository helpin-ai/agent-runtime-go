# Agent Runtime Go SDK

Public Go contracts and HTTP client for Agent Runtime hosts.

This package contains only wire contracts, event helpers, host callback DTOs,
and a small `/v1` HTTP client. Product lifecycle policy such as billing,
projection, finalizers, automations, and UI persistence belongs in the host
application.

## Install

```bash
go get github.com/helpin-ai/agent-runtime-go@v0.1.0
```

## Test

```bash
go vet ./...
go test ./...
```
