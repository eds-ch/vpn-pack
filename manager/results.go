package main

import (
	"encoding/json"
	"log/slog"

	"unifi-tailscale/manager/service"
)

// OperationResponse is a lightweight response for operations that don't return entity data.
type OperationResponse struct {
	OK bool `json:"ok"`
}

// SSE event structs for type-safe broadcasting.

type SSEStatusEvent = stateData

type SSEHealthEvent struct {
	FirewallHealth *FirewallHealth    `json:"firewallHealth,omitempty"`
	Integration    *service.IntegrationStatus `json:"integrationStatus,omitempty"`
}

type SSEDNSEvent struct {
	Enabled bool   `json:"enabled"`
	Domain  string `json:"domain,omitempty"`
}

// BroadcastEvent marshals payload and broadcasts as a named SSE event.
// If event is empty, broadcasts as unnamed (default status) event.
func BroadcastEvent[T any](hub SSEHub, event string, payload T) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("sse marshal failed", "event", event, "err", err)
		return
	}
	if event == "" {
		hub.Broadcast(data)
	} else {
		hub.BroadcastNamed(event, data)
	}
}
