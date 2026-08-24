package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/YangKeao/haro-bot/internal/sandbox"
)

func (s *Server) handleSandboxEvents(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxes(w) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "streaming response is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Retry-After", "2")
	_, _ = fmt.Fprint(w, "retry: 2000\n\n")

	items, err := s.sandboxes.List(r.Context())
	if err != nil {
		writeSandboxStreamEvent(w, "error", map[string]string{"message": err.Error()})
		flusher.Flush()
		return
	}
	writeSandboxStreamEvent(w, "snapshot", map[string]any{"sandboxes": items})
	flusher.Flush()
	known := sandboxFingerprints(items)

	ticker := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-ticker.C:
			items, err := s.sandboxes.List(r.Context())
			if err != nil {
				writeSandboxStreamEvent(w, "error", map[string]string{"message": err.Error()})
				flusher.Flush()
				continue
			}
			next := sandboxFingerprints(items)
			for _, item := range items {
				if known[item.ID] != next[item.ID] {
					writeSandboxStreamEvent(w, "sandbox", map[string]any{"sandbox": item})
				}
			}
			for id := range known {
				if _, ok := next[id]; !ok {
					writeSandboxStreamEvent(w, "removed", map[string]int64{"id": id})
				}
			}
			known = next
			flusher.Flush()
		}
	}
}

func writeSandboxStreamEvent(w http.ResponseWriter, event string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

func sandboxFingerprints(items []sandbox.Profile) map[int64]string {
	result := make(map[int64]string, len(items))
	for _, item := range items {
		copy := item
		if item.RuntimeDetails != nil {
			details := *item.RuntimeDetails
			details.ObservedAt = time.Time{}
			copy.RuntimeDetails = &details
		}
		data, _ := json.Marshal(copy)
		result[item.ID] = string(data)
	}
	return result
}
