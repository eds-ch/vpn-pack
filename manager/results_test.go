package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

		BroadcastEvent(h, "health", HealthSnapshot{Status: StatusHealthy})

		select {
		case msg := <-ch:
			assert.Equal(t, "health", msg.Event)
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})
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
