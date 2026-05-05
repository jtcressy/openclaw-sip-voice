package protocol

import (
	"encoding/base64"
	"errors"
	"time"
)

const (
	Version          = "1.0"
	DefaultBridgeURL = "ws://127.0.0.1:9077"
)

type AudioFormat struct {
	Format          string `json:"format"`
	SampleRateHz    int    `json:"sampleRateHz"`
	Channels        int    `json:"channels"`
	FrameDurationMs int    `json:"frameDurationMs"`
	PayloadEncoding string `json:"payloadEncoding"`
}

type AudioFrame struct {
	Format          string `json:"format"`
	SampleRateHz    int    `json:"sampleRateHz"`
	Channels        int    `json:"channels"`
	FrameDurationMs int    `json:"frameDurationMs"`
	PayloadEncoding string `json:"payloadEncoding"`
	Payload         string `json:"payload,omitempty"`
}

type RemoteParty struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName,omitempty"`
}

const CanonicalAudioFrameBytes = 160

var (
	ErrUnsupportedAudioFormat = errors.New("unsupported audio format")
	ErrInvalidAudioPayload    = errors.New("invalid audio payload")
)

func CanonicalAudioFormat() AudioFormat {
	return AudioFormat{
		Format:          "g711_ulaw",
		SampleRateHz:    8000,
		Channels:        1,
		FrameDurationMs: 20,
		PayloadEncoding: "base64",
	}
}

func NewAudioFrame(payload string) AudioFrame {
	format := CanonicalAudioFormat()
	return AudioFrame{
		Format:          format.Format,
		SampleRateHz:    format.SampleRateHz,
		Channels:        format.Channels,
		FrameDurationMs: format.FrameDurationMs,
		PayloadEncoding: format.PayloadEncoding,
		Payload:         payload,
	}
}

func NewAudioFrameFromPayload(payload []byte) AudioFrame {
	return NewAudioFrame(base64.StdEncoding.EncodeToString(payload))
}

func (f AudioFrame) AudioFormat() AudioFormat {
	return AudioFormat{
		Format:          f.Format,
		SampleRateHz:    f.SampleRateHz,
		Channels:        f.Channels,
		FrameDurationMs: f.FrameDurationMs,
		PayloadEncoding: f.PayloadEncoding,
	}
}

func DecodeCanonicalAudioPayload(frame AudioFrame) ([]byte, error) {
	if frame.AudioFormat() != CanonicalAudioFormat() {
		return nil, ErrUnsupportedAudioFormat
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(frame.Payload)
	if err != nil || len(payload) != CanonicalAudioFrameBytes {
		return nil, ErrInvalidAudioPayload
	}
	return payload, nil
}

type TransportInfo struct {
	Kind       string `json:"kind"`
	DefaultURL string `json:"defaultUrl"`
}

type Capabilities struct {
	InboundCalls     bool `json:"inboundCalls"`
	OutboundCalls    bool `json:"outboundCalls"`
	BargeIn          bool `json:"bargeIn"`
	ClearQueuedAudio bool `json:"clearQueuedAudio"`
}

type HelloEvent struct {
	ProtocolVersion           string        `json:"protocolVersion"`
	Type                      string        `json:"type"`
	SentAt                    string        `json:"sentAt"`
	BridgeID                  string        `json:"bridgeId"`
	BridgeVersion             string        `json:"bridgeVersion"`
	Transport                 TransportInfo `json:"transport"`
	SupportedProtocolVersions []string      `json:"supportedProtocolVersions"`
	Audio                     AudioFormat   `json:"audio"`
	Capabilities              Capabilities  `json:"capabilities"`
}

type BridgeState string

const (
	BridgeStateStarting BridgeState = "starting"
	BridgeStateReady    BridgeState = "ready"
	BridgeStateDegraded BridgeState = "degraded"
	BridgeStateOffline  BridgeState = "offline"
)

type RegistrationState string

const (
	RegistrationStateRegistered   RegistrationState = "registered"
	RegistrationStateUnregistered RegistrationState = "unregistered"
	RegistrationStateRegistering  RegistrationState = "registering"
	RegistrationStateError        RegistrationState = "error"
)

type RegistrationStatus struct {
	State      RegistrationState `json:"state"`
	Label      string            `json:"label,omitempty"`
	ReasonCode string            `json:"reasonCode,omitempty"`
	Message    string            `json:"message,omitempty"`
}

type CallSummary struct {
	CallID    string `json:"callId"`
	Direction string `json:"direction"`
	State     string `json:"state"`
}

const (
	CallDirectionInbound  = "inbound"
	CallDirectionOutbound = "outbound"

	CallStateRinging = "ringing"
	CallStateDialing = "dialing"
	CallStateActive  = "active"
	CallStateEnding  = "ending"
)

type StatusEvent struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Type            string             `json:"type"`
	SentAt          string             `json:"sentAt"`
	BridgeState     BridgeState        `json:"bridgeState"`
	Registration    RegistrationStatus `json:"registration"`
	ActiveCalls     []CallSummary      `json:"activeCalls"`
}

type StatusSnapshot struct {
	BridgeState  BridgeState
	Registration RegistrationStatus
	ActiveCalls  []CallSummary
}

type ErrorEvent struct {
	ProtocolVersion string       `json:"protocolVersion"`
	Type            string       `json:"type"`
	SentAt          string       `json:"sentAt"`
	Fatal           bool         `json:"fatal"`
	Error           ErrorPayload `json:"error"`
}

type ErrorPayload struct {
	Code                     string   `json:"code"`
	Message                  string   `json:"message"`
	Retryable                bool     `json:"retryable"`
	CommandID                string   `json:"commandId,omitempty"`
	CallID                   string   `json:"callId,omitempty"`
	ExpectedProtocolVersions []string `json:"expectedProtocolVersions,omitempty"`
}

type CallStartEvent struct {
	ProtocolVersion      string      `json:"protocolVersion"`
	Type                 string      `json:"type"`
	SentAt               string      `json:"sentAt"`
	CallID               string      `json:"callId"`
	Direction            string      `json:"direction"`
	State                string      `json:"state"`
	Remote               RemoteParty `json:"remote"`
	Audio                AudioFormat `json:"audio"`
	RequestedByCommandID string      `json:"requestedByCommandId,omitempty"`
}

type CallEndEvent struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Type            string         `json:"type"`
	SentAt          string         `json:"sentAt"`
	CallID          string         `json:"callId"`
	Outcome         string         `json:"outcome"`
	DurationMs      int            `json:"durationMs,omitempty"`
	Reason          *CallEndReason `json:"reason,omitempty"`
}

type CallEndReason struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

type AudioInEvent struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Type            string     `json:"type"`
	SentAt          string     `json:"sentAt"`
	CallID          string     `json:"callId"`
	Sequence        int        `json:"sequence"`
	TimestampMs     int        `json:"timestampMs"`
	Audio           AudioFrame `json:"audio"`
}

type Command struct {
	ProtocolVersion string      `json:"protocolVersion"`
	Type            string      `json:"type"`
	SentAt          string      `json:"sentAt"`
	CommandID       string      `json:"commandId"`
	CallID          string      `json:"callId,omitempty"`
	Remote          RemoteParty `json:"remote,omitempty"`
	Audio           AudioFrame  `json:"audio,omitempty"`
	Sequence        int         `json:"sequence,omitempty"`
	Scope           string      `json:"scope,omitempty"`
	Reason          string      `json:"reason,omitempty"`
}

func DefaultCapabilities() Capabilities {
	return Capabilities{
		InboundCalls:     true,
		OutboundCalls:    true,
		BargeIn:          false,
		ClearQueuedAudio: false,
	}
}

func MediaCapabilities() Capabilities {
	return Capabilities{
		InboundCalls:     true,
		OutboundCalls:    true,
		BargeIn:          true,
		ClearQueuedAudio: true,
	}
}

func NewHelloEvent(sentAt time.Time, bridgeID string, bridgeVersion string) HelloEvent {
	return NewHelloEventWithCapabilities(sentAt, bridgeID, bridgeVersion, DefaultCapabilities())
}

func NewHelloEventWithCapabilities(sentAt time.Time, bridgeID string, bridgeVersion string, capabilities Capabilities) HelloEvent {
	return HelloEvent{
		ProtocolVersion: Version,
		Type:            "hello",
		SentAt:          formatSentAt(sentAt),
		BridgeID:        bridgeID,
		BridgeVersion:   bridgeVersion,
		Transport: TransportInfo{
			Kind:       "websocket",
			DefaultURL: DefaultBridgeURL,
		},
		SupportedProtocolVersions: []string{Version},
		Audio:                     CanonicalAudioFormat(),
		Capabilities:              capabilities,
	}
}

func NewStatusEvent(sentAt time.Time, snapshot StatusSnapshot) StatusEvent {
	activeCalls := snapshot.ActiveCalls
	if activeCalls == nil {
		activeCalls = []CallSummary{}
	}
	return StatusEvent{
		ProtocolVersion: Version,
		Type:            "status",
		SentAt:          formatSentAt(sentAt),
		BridgeState:     snapshot.BridgeState,
		Registration:    snapshot.Registration,
		ActiveCalls:     activeCalls,
	}
}

func NewErrorEvent(sentAt time.Time, code string, message string, retryable bool, fatal bool, commandID string) ErrorEvent {
	payload := ErrorPayload{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		CommandID: commandID,
	}
	if code == "unsupported_protocol_version" {
		payload.ExpectedProtocolVersions = []string{Version}
	}
	return ErrorEvent{
		ProtocolVersion: Version,
		Type:            "error",
		SentAt:          formatSentAt(sentAt),
		Fatal:           fatal,
		Error:           payload,
	}
}

func NewCallStartEvent(sentAt time.Time, callID string, direction string, state string, remote RemoteParty, requestedByCommandID string) CallStartEvent {
	return CallStartEvent{
		ProtocolVersion:      Version,
		Type:                 "call.start",
		SentAt:               formatSentAt(sentAt),
		CallID:               callID,
		Direction:            direction,
		State:                state,
		Remote:               remote,
		Audio:                CanonicalAudioFormat(),
		RequestedByCommandID: requestedByCommandID,
	}
}

func NewCallEndEvent(sentAt time.Time, callID string, outcome string, durationMs int, reason *CallEndReason) CallEndEvent {
	return CallEndEvent{
		ProtocolVersion: Version,
		Type:            "call.end",
		SentAt:          formatSentAt(sentAt),
		CallID:          callID,
		Outcome:         outcome,
		DurationMs:      durationMs,
		Reason:          reason,
	}
}

func NewAudioInEvent(sentAt time.Time, callID string, sequence int, timestampMs int, payload []byte) AudioInEvent {
	return AudioInEvent{
		ProtocolVersion: Version,
		Type:            "audio.in",
		SentAt:          formatSentAt(sentAt),
		CallID:          callID,
		Sequence:        sequence,
		TimestampMs:     timestampMs,
		Audio:           NewAudioFrameFromPayload(payload),
	}
}

func formatSentAt(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}
