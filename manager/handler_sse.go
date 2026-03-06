package main

import (
	"fmt"
	"net/http"
)

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
