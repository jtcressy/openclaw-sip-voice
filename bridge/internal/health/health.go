package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
)

type SnapshotProvider interface {
	Snapshot() protocol.StatusSnapshot
}

func NewHandler(provider SnapshotProvider) http.Handler {
	mux := http.NewServeMux()
	handler := &handler{
		provider:  provider,
		startedAt: time.Now().UTC(),
	}
	mux.HandleFunc("/healthz", handler.healthz)
	mux.HandleFunc("/readyz", handler.readyz)
	mux.HandleFunc("/metrics", handler.metrics)
	return mux
}

type handler struct {
	provider  SnapshotProvider
	startedAt time.Time
}

type healthResponse struct {
	OK                bool                 `json:"ok"`
	BridgeState       protocol.BridgeState `json:"bridgeState"`
	RegistrationState string               `json:"registrationState"`
	ActiveCalls       int                  `json:"activeCalls"`
	UptimeSeconds     int64                `json:"uptimeSeconds"`
}

func (h *handler) healthz(w http.ResponseWriter, _ *http.Request) {
	snapshot := h.provider.Snapshot()
	h.writeHealth(w, snapshot, snapshot.BridgeState != protocol.BridgeStateOffline)
}

func (h *handler) readyz(w http.ResponseWriter, _ *http.Request) {
	snapshot := h.provider.Snapshot()
	h.writeHealth(w, snapshot, isReady(snapshot))
}

func (h *handler) metrics(w http.ResponseWriter, _ *http.Request) {
	snapshot := h.provider.Snapshot()
	up := 1
	if snapshot.BridgeState == protocol.BridgeStateOffline {
		up = 0
	}
	ready := 0
	if isReady(snapshot) {
		ready = 1
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "# TYPE openclaw_bridge_up gauge\nopenclaw_bridge_up %d\n", up)
	_, _ = fmt.Fprintf(w, "# TYPE openclaw_bridge_ready gauge\nopenclaw_bridge_ready %d\n", ready)
	_, _ = fmt.Fprintf(w, "# TYPE openclaw_bridge_active_calls gauge\nopenclaw_bridge_active_calls %d\n", len(snapshot.ActiveCalls))
}

func (h *handler) writeHealth(w http.ResponseWriter, snapshot protocol.StatusSnapshot, ok bool) {
	status := http.StatusOK
	if !ok {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(healthResponse{
		OK:                ok,
		BridgeState:       snapshot.BridgeState,
		RegistrationState: string(snapshot.Registration.State),
		ActiveCalls:       len(snapshot.ActiveCalls),
		UptimeSeconds:     int64(time.Since(h.startedAt).Seconds()),
	})
}

func isReady(snapshot protocol.StatusSnapshot) bool {
	return snapshot.BridgeState == protocol.BridgeStateReady &&
		snapshot.Registration.State == protocol.RegistrationStateRegistered
}
