package sse

import (
	"fmt"
	"sync"
	"sync/atomic"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

type Hub struct {
	mu      sync.Mutex
	clients map[chan domain.SSEMessage]struct{}
	state   atomic.Value
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan domain.SSEMessage]struct{}),
	}
}

func (h *Hub) Subscribe() (chan domain.SSEMessage, func(), error) {
	ch := make(chan domain.SSEMessage, config.SSEChannelBuffer)
	h.mu.Lock()
	if len(h.clients) >= config.MaxSSEClients {
		h.mu.Unlock()
		return nil, nil, fmt.Errorf("too many SSE connections (max %d)", config.MaxSSEClients)
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.clients, ch)
			close(ch)
			h.mu.Unlock()
			for range ch {
			}
		})
	}
	return ch, unsubscribe, nil
}

func (h *Hub) Broadcast(data []byte) {
	h.state.Store(data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- domain.SSEMessage{Data: data}:
		default:
		}
	}
}

func (h *Hub) BroadcastNamed(event string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- domain.SSEMessage{Event: event, Data: data}:
		default:
		}
	}
}

func (h *Hub) CurrentState() []byte {
	v := h.state.Load()
	if v == nil {
		return nil
	}
	data, ok := v.([]byte)
	if !ok {
		return nil
	}
	return data
}
