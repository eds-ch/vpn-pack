package domain

import (
	"encoding/json"
	"log/slog"
)

type OperationResponse struct {
	OK bool `json:"ok"`
}

type SSEDNSEvent struct {
	Enabled bool   `json:"enabled"`
	Domain  string `json:"domain,omitempty"`
}

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
