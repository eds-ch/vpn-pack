package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepError(t *testing.T) {
	e := StepError{Step: "create_zone", Err: fmt.Errorf("timeout")}
	assert.Equal(t, "create_zone: timeout", e.Error())
}

func TestFirewallSetupResult_OK(t *testing.T) {
	tests := []struct {
		name   string
		result FirewallSetupResult
		want   bool
	}{
		{"all true no errors", FirewallSetupResult{ZoneCreated: true, PoliciesReady: true, UDAPIApplied: true}, true},
		{"zone false", FirewallSetupResult{PoliciesReady: true, UDAPIApplied: true}, false},
		{"policies false", FirewallSetupResult{ZoneCreated: true, UDAPIApplied: true}, false},
		{"udapi false", FirewallSetupResult{ZoneCreated: true, PoliciesReady: true}, false},
		{"has errors", FirewallSetupResult{
			ZoneCreated: true, PoliciesReady: true, UDAPIApplied: true,
			Errors: []StepError{{Step: "test", Err: fmt.Errorf("fail")}},
		}, false},
		{"zero value", FirewallSetupResult{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.OK())
		})
	}
}

func TestFirewallSetupResult_Degraded(t *testing.T) {
	tests := []struct {
		name   string
		result FirewallSetupResult
		want   bool
	}{
		{"zone+policies ok, udapi missing", FirewallSetupResult{ZoneCreated: true, PoliciesReady: true}, true},
		{"fully ok", FirewallSetupResult{ZoneCreated: true, PoliciesReady: true, UDAPIApplied: true}, false},
		{"zone missing", FirewallSetupResult{PoliciesReady: true}, false},
		{"policies missing", FirewallSetupResult{ZoneCreated: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.Degraded())
		})
	}
}

func TestFirewallSetupResult_Err(t *testing.T) {
	t.Run("no errors returns nil", func(t *testing.T) {
		r := &FirewallSetupResult{ZoneCreated: true}
		assert.NoError(t, r.Err())
	})

	t.Run("single error", func(t *testing.T) {
		r := &FirewallSetupResult{}
		r.addError("zone", fmt.Errorf("not found"))
		err := r.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "zone: not found")
	})

	t.Run("multiple errors joined", func(t *testing.T) {
		r := &FirewallSetupResult{}
		r.addError("zone", fmt.Errorf("fail1"))
		r.addError("policy", fmt.Errorf("fail2"))
		err := r.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "zone: fail1")
		assert.Contains(t, err.Error(), "policy: fail2")
	})
}

func TestNewFirewallStatusBrief(t *testing.T) {
	t.Run("nil input returns nil", func(t *testing.T) {
		assert.Nil(t, NewFirewallStatusBrief(nil))
	})

	t.Run("maps fields correctly", func(t *testing.T) {
		r := &FirewallSetupResult{
			ZoneCreated:   true,
			PoliciesReady: true,
			UDAPIApplied:  false,
		}
		b := NewFirewallStatusBrief(r)
		require.NotNil(t, b)
		assert.True(t, b.ZoneCreated)
		assert.True(t, b.PoliciesReady)
		assert.False(t, b.UDAPIApplied)
		assert.Empty(t, b.Errors)
	})

	t.Run("includes error strings", func(t *testing.T) {
		r := &FirewallSetupResult{
			Errors: []StepError{{Step: "udapi", Err: fmt.Errorf("timeout")}},
		}
		b := NewFirewallStatusBrief(r)
		require.Len(t, b.Errors, 1)
		assert.Equal(t, "udapi: timeout", b.Errors[0])
	})
}

func TestBroadcastEvent(t *testing.T) {
	t.Run("unnamed event uses Broadcast", func(t *testing.T) {
		h := NewHub()
		ch, unsub, err := h.Subscribe()
		require.NoError(t, err)
		defer unsub()

		BroadcastEvent(h, "", map[string]string{"key": "val"})

		select {
		case msg := <-ch:
			assert.Empty(t, msg.Event)
			var got map[string]string
			require.NoError(t, json.Unmarshal(msg.Data, &got))
			assert.Equal(t, "val", got["key"])
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})

	t.Run("named event uses BroadcastNamed", func(t *testing.T) {
		h := NewHub()
		ch, unsub, err := h.Subscribe()
		require.NoError(t, err)
		defer unsub()

		BroadcastEvent(h, "health", SSEHealthEvent{})

		select {
		case msg := <-ch:
			assert.Equal(t, "health", msg.Event)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})
}

func TestAPIResponseJSON(t *testing.T) {
	resp := APIResponse[string]{
		Data:   "hello",
		Status: "ok",
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "hello", parsed["data"])
	assert.Equal(t, "ok", parsed["status"])
	assert.Nil(t, parsed["firewall"])
}

func TestSSEDNSEventJSON(t *testing.T) {
	evt := SSEDNSEvent{Enabled: true, Domain: "example.ts.net"}
	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, true, parsed["enabled"])
	assert.Equal(t, "example.ts.net", parsed["domain"])
}
