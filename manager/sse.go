package main

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
)

type sseMessage struct {
	Event string
	Data  []byte
}

type Hub struct {
	mu      sync.Mutex
	clients map[chan sseMessage]struct{}
	state   atomic.Value
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan sseMessage]struct{}),
	}
}

func (h *Hub) Subscribe() (chan sseMessage, func(), error) {
	ch := make(chan sseMessage, sseChannelBuffer)
	h.mu.Lock()
	if len(h.clients) >= maxSSEClients {
		h.mu.Unlock()
		return nil, nil, fmt.Errorf("too many SSE connections (max %d)", maxSSEClients)
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		delete(h.clients, ch)
		close(ch)
		h.mu.Unlock()
		for range ch { // drain buffered messages
		}
	}
	return ch, unsubscribe, nil
}

func (h *Hub) Broadcast(data []byte) {
	h.state.Store(data)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- sseMessage{Data: data}:
		default:
		}
	}
}

func (h *Hub) BroadcastNamed(event string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- sseMessage{Event: event, Data: data}:
		default:
		}
	}
}

func (h *Hub) CurrentState() []byte {
	v := h.state.Load()
	if v == nil {
		return nil
	}
	return v.([]byte)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ch, unsubscribe, err := s.hub.Subscribe()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if current := s.hub.CurrentState(); current != nil {
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(current)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg.Event != "" {
				_, _ = fmt.Fprintf(w, "event: %s\n", msg.Event)
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(msg.Data)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}
