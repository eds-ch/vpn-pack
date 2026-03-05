package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"unifi-tailscale/manager/service"
)

type StepError struct {
	Step string
	Err  error
}

func (e StepError) Error() string {
	return fmt.Sprintf("%s: %s", e.Step, e.Err)
}

func (e StepError) Unwrap() error {
	return e.Err
}

type FirewallSetupResult struct {
	ZoneCreated   bool
	ZoneID        string
	ZoneName      string
	PoliciesReady bool
	PolicyIDs     []string
	UDAPIApplied  bool
	ChainPrefix   string
	Errors        []StepError
}

func (r *FirewallSetupResult) OK() bool {
	return r.ZoneCreated && r.PoliciesReady && r.UDAPIApplied && len(r.Errors) == 0
}

func (r *FirewallSetupResult) Degraded() bool {
	return r.ZoneCreated && r.PoliciesReady && !r.UDAPIApplied
}

func (r *FirewallSetupResult) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	msgs := make([]string, len(r.Errors))
	for i, e := range r.Errors {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("firewall setup: %s", strings.Join(msgs, "; "))
}

func (r *FirewallSetupResult) addError(step string, err error) {
	r.Errors = append(r.Errors, StepError{Step: step, Err: err})
}

// APIResponse wraps any response with status and optional firewall details.
type APIResponse[T any] struct {
	Data     T                   `json:"data,omitempty"`
	Status   string              `json:"status"`
	Firewall *FirewallStatusBrief `json:"firewall,omitempty"`
}

type FirewallStatusBrief struct {
	ZoneCreated   bool     `json:"zoneCreated"`
	PoliciesReady bool     `json:"policiesReady"`
	UDAPIApplied  bool     `json:"udapiApplied"`
	Errors        []string `json:"errors,omitempty"`
}

func NewFirewallStatusBrief(r *FirewallSetupResult) *FirewallStatusBrief {
	if r == nil {
		return nil
	}
	b := &FirewallStatusBrief{
		ZoneCreated:   r.ZoneCreated,
		PoliciesReady: r.PoliciesReady,
		UDAPIApplied:  r.UDAPIApplied,
	}
	for _, e := range r.Errors {
		b.Errors = append(b.Errors, e.Error())
	}
	return b
}

// TunnelResponse extends the tunnel response with firewall status.
// Used by both Create and Update handlers.
type TunnelResponse struct {
	wgS2sTunnelResponse
	Status   string              `json:"status,omitempty"`
	Firewall *FirewallStatusBrief `json:"firewall,omitempty"`
}

// OperationResponse is a lightweight response for operations that don't return entity data.
type OperationResponse struct {
	OK       bool                `json:"ok"`
	Status   string              `json:"status,omitempty"`
	Firewall *FirewallStatusBrief `json:"firewall,omitempty"`
}

func firewallStatus(zoneResult *FirewallSetupResult, fwErr error) string {
	if (zoneResult == nil || zoneResult.Err() == nil) && fwErr == nil {
		return "ok"
	}
	return "partial"
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
