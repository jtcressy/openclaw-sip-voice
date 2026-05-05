package health

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
)

func TestHealthzReportsProcessStateWithoutCredentialFields(t *testing.T) {
	handler := NewHandler(staticProvider{snapshot: protocol.StatusSnapshot{
		BridgeState: protocol.BridgeStateDegraded,
		Registration: protocol.RegistrationStatus{
			State:      protocol.RegistrationStateUnregistered,
			ReasonCode: "not_configured",
			Message:    "No voice line is registered.",
		},
		ActiveCalls: []protocol.CallSummary{},
	}})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"sip", "credential", "password", "unifi"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("health response leaked forbidden field/value %q in %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"bridgeState":"degraded"`) {
		t.Fatalf("health response did not include degraded state: %s", body)
	}
}

func TestReadyzRequiresReadyRegisteredState(t *testing.T) {
	handler := NewHandler(staticProvider{snapshot: protocol.StatusSnapshot{
		BridgeState: protocol.BridgeStateDegraded,
		Registration: protocol.RegistrationStatus{
			State:      protocol.RegistrationStateError,
			ReasonCode: "auth_failed",
			Message:    "SIP registration failed.",
		},
		ActiveCalls: []protocol.CallSummary{},
	}})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(recorder.Body.String(), `"ok":false`) {
		t.Fatalf("ready response did not report ok false: %s", recorder.Body.String())
	}
}

func TestReadyzReportsReadyWhenRegistered(t *testing.T) {
	handler := NewHandler(staticProvider{snapshot: protocol.StatusSnapshot{
		BridgeState: protocol.BridgeStateReady,
		Registration: protocol.RegistrationStatus{
			State: protocol.RegistrationStateRegistered,
		},
		ActiveCalls: []protocol.CallSummary{},
	}})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !strings.Contains(recorder.Body.String(), `"ok":true`) {
		t.Fatalf("ready response did not report ok true: %s", recorder.Body.String())
	}
}

type staticProvider struct {
	snapshot protocol.StatusSnapshot
}

func (p staticProvider) Snapshot() protocol.StatusSnapshot {
	return p.snapshot
}
