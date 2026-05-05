package runtime

import (
	"regexp"
	"sync"
	"unicode"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
)

const defaultRegistrationLabel = "default-line"

var safeReasonCodePattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

type State struct {
	mu       sync.RWMutex
	snapshot protocol.StatusSnapshot
}

func NewState(cfg config.Config) *State {
	registration := protocol.RegistrationStatus{
		State:      protocol.RegistrationStateUnregistered,
		ReasonCode: "not_configured",
		Message:    "No voice line is registered.",
	}
	if cfg.UniFiConfigured() {
		registration = protocol.RegistrationStatus{
			State:      protocol.RegistrationStateUnregistered,
			Label:      defaultRegistrationLabel,
			ReasonCode: "not_registered",
			Message:    "Registration has not started.",
		}
	}
	return &State{
		snapshot: protocol.StatusSnapshot{
			BridgeState:  protocol.BridgeStateDegraded,
			Registration: registration,
			ActiveCalls:  []protocol.CallSummary{},
		},
	}
}

func (s *State) Snapshot() protocol.StatusSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeCalls := make([]protocol.CallSummary, len(s.snapshot.ActiveCalls))
	copy(activeCalls, s.snapshot.ActiveCalls)
	snapshot := s.snapshot
	snapshot.ActiveCalls = activeCalls
	return snapshot
}

func (s *State) SetSnapshot(snapshot protocol.StatusSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if snapshot.ActiveCalls == nil {
		snapshot.ActiveCalls = []protocol.CallSummary{}
	}
	s.snapshot = snapshot
}

func (s *State) UpsertCall(call protocol.CallSummary) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.ActiveCalls == nil {
		s.snapshot.ActiveCalls = []protocol.CallSummary{}
	}
	for i := range s.snapshot.ActiveCalls {
		if s.snapshot.ActiveCalls[i].CallID == call.CallID {
			s.snapshot.ActiveCalls[i] = call
			return
		}
	}
	s.snapshot.ActiveCalls = append(s.snapshot.ActiveCalls, call)
}

func (s *State) RemoveCall(callID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.ActiveCalls == nil {
		s.snapshot.ActiveCalls = []protocol.CallSummary{}
		return
	}
	activeCalls := s.snapshot.ActiveCalls[:0]
	for _, call := range s.snapshot.ActiveCalls {
		if call.CallID != callID {
			activeCalls = append(activeCalls, call)
		}
	}
	if activeCalls == nil {
		activeCalls = []protocol.CallSummary{}
	}
	s.snapshot.ActiveCalls = activeCalls
}

func (s *State) SetRegistrationNotConfigured() {
	s.setRegistration(protocol.BridgeStateDegraded, protocol.RegistrationStatus{
		State:      protocol.RegistrationStateUnregistered,
		ReasonCode: "not_configured",
		Message:    "No voice line is registered.",
	})
}

func (s *State) SetRegistrationRegistering(label string) {
	s.setRegistration(protocol.BridgeStateDegraded, protocol.RegistrationStatus{
		State:      protocol.RegistrationStateRegistering,
		Label:      registrationLabel(label),
		ReasonCode: "registration_starting",
		Message:    "SIP registration is starting.",
	})
}

func (s *State) SetRegistrationRegistered(label string) {
	s.setRegistration(protocol.BridgeStateReady, protocol.RegistrationStatus{
		State:   protocol.RegistrationStateRegistered,
		Label:   registrationLabel(label),
		Message: "Voice line is registered.",
	})
}

func (s *State) SetRegistrationError(label string, reasonCode string, message string) {
	s.setRegistration(protocol.BridgeStateDegraded, protocol.RegistrationStatus{
		State:      protocol.RegistrationStateError,
		Label:      registrationLabel(label),
		ReasonCode: safeReasonCode(reasonCode, "registration_error"),
		Message:    safeMessage(message, "SIP registration failed."),
	})
}

func (s *State) SetRegistrationUnregistered(label string, reasonCode string, message string) {
	s.setRegistration(protocol.BridgeStateDegraded, protocol.RegistrationStatus{
		State:      protocol.RegistrationStateUnregistered,
		Label:      registrationLabel(label),
		ReasonCode: safeReasonCode(reasonCode, "not_registered"),
		Message:    safeMessage(message, "No voice line is registered."),
	})
}

func (s *State) setRegistration(bridgeState protocol.BridgeState, registration protocol.RegistrationStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot.ActiveCalls == nil {
		s.snapshot.ActiveCalls = []protocol.CallSummary{}
	}
	s.snapshot.BridgeState = bridgeState
	s.snapshot.Registration = registration
}

func registrationLabel(label string) string {
	if label == "" {
		return defaultRegistrationLabel
	}
	return label
}

func safeReasonCode(value string, fallback string) string {
	if safeReasonCodePattern.MatchString(value) {
		return value
	}
	return fallback
}

func safeMessage(value string, fallback string) string {
	cleaned := make([]rune, 0, len(value))
	for _, r := range value {
		if unicode.IsControl(r) {
			if len(cleaned) == 0 || cleaned[len(cleaned)-1] != ' ' {
				cleaned = append(cleaned, ' ')
			}
			continue
		}
		cleaned = append(cleaned, r)
		if len(cleaned) >= 160 {
			break
		}
	}
	for len(cleaned) > 0 && cleaned[0] == ' ' {
		cleaned = cleaned[1:]
	}
	for len(cleaned) > 0 && cleaned[len(cleaned)-1] == ' ' {
		cleaned = cleaned[:len(cleaned)-1]
	}
	if len(cleaned) == 0 {
		return fallback
	}
	return string(cleaned)
}
