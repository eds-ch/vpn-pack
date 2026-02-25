package main

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHub(t *testing.T) {
	t.Run("CurrentState returns nil initially", func(t *testing.T) {
		h := NewHub()
		assert.Nil(t, h.CurrentState())
	})

	t.Run("Subscribe returns channel and unsubscribe", func(t *testing.T) {
		h := NewHub()
		ch, unsub, err := h.Subscribe()
		require.NoError(t, err)
		assert.NotNil(t, ch)
		assert.NotNil(t, unsub)
		unsub()
	})

	t.Run("Broadcast stores state", func(t *testing.T) {
		h := NewHub()
		h.Broadcast([]byte("hello"))
		assert.Equal(t, []byte("hello"), h.CurrentState())
	})

	t.Run("Broadcast delivers to subscriber", func(t *testing.T) {
		h := NewHub()
		ch, unsub, err := h.Subscribe()
		require.NoError(t, err)
		defer unsub()

		h.Broadcast([]byte("data"))
		select {
		case msg := <-ch:
			assert.Equal(t, []byte("data"), msg.Data)
			assert.Empty(t, msg.Event)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for broadcast")
		}
	})

	t.Run("Broadcast to 3 subscribers", func(t *testing.T) {
		h := NewHub()
		channels := make([]chan sseMessage, 3)
		unsubs := make([]func(), 3)
		for i := 0; i < 3; i++ {
			ch, unsub, err := h.Subscribe()
			require.NoError(t, err)
			channels[i] = ch
			unsubs[i] = unsub
		}
		defer func() {
			for _, u := range unsubs {
				u()
			}
		}()

		h.Broadcast([]byte("multi"))
		for i, ch := range channels {
			select {
			case msg := <-ch:
				assert.Equal(t, []byte("multi"), msg.Data, "subscriber %d", i)
			case <-time.After(time.Second):
				t.Fatalf("timeout on subscriber %d", i)
			}
		}
	})

	t.Run("Unsubscribe then Broadcast", func(t *testing.T) {
		h := NewHub()
		ch, unsub, err := h.Subscribe()
		require.NoError(t, err)
		unsub()

		h.Broadcast([]byte("after-unsub"))

		// Channel is closed by unsub(), so receive should return zero-value immediately
		select {
		case msg, ok := <-ch:
			assert.False(t, ok, "channel should be closed after unsubscribe")
			assert.Empty(t, msg.Event, "closed channel should return zero-value")
			assert.Nil(t, msg.Data, "closed channel should return nil data")
		case <-time.After(time.Second):
			t.Fatal("expected closed channel to return immediately")
		}
	})

	t.Run("max clients limit", func(t *testing.T) {
		h := NewHub()
		unsubs := make([]func(), 0, maxSSEClients)
		for i := 0; i < maxSSEClients; i++ {
			_, unsub, err := h.Subscribe()
			require.NoError(t, err, "subscribe %d", i)
			unsubs = append(unsubs, unsub)
		}

		_, _, err := h.Subscribe()
		assert.Error(t, err)

		for _, u := range unsubs {
			u()
		}
	})

	t.Run("concurrent subscribe and broadcast", func(t *testing.T) {
		h := NewHub()
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch, unsub, err := h.Subscribe()
				if err != nil {
					return
				}
				h.Broadcast([]byte("concurrent"))
				select {
				case <-ch:
				case <-time.After(100 * time.Millisecond):
				}
				unsub()
			}()
		}
		wg.Wait()
	})
}
