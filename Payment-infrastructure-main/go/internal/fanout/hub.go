// Package fanout implements SSE broadcast to the console (docs/00 §5: its own goroutine
// pool specifically so broadcast serialisation cannot touch the scoring heap — fixes F-68).
package fanout

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// heartbeatInterval keeps the connection demonstrably alive (and any intermediate proxy
// from timing it out) even when no decision has fired since the client connected.
const heartbeatInterval = 15 * time.Second

type Hub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: map[chan []byte]struct{}{}}
}

// Publish is non-blocking: a slow client gets dropped frames, never a blocked scorer.
func (h *Hub) Publish(event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	frame := append([]byte("event: "+event+"\ndata: "), b...)
	frame = append(frame, '\n', '\n')

	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- frame:
		default:
			// coalesced + sampled (docs/00 §6): drop rather than block.
		}
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	// Send headers immediately: without this, nothing reaches the client — not even the
	// response line — until the first decision fires, so EventSource sits in CONNECTING
	// indefinitely on an idle stream instead of reporting itself open.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case <-ticker.C:
			if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case frame := <-ch:
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
