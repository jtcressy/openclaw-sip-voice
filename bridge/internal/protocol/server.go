package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrUnsupportedProtocolVersion = errors.New("unsupported protocol version")
	commandIDPattern              = regexp.MustCompile(`^cmd_[A-Za-z0-9._:-]{6,96}$`)
	callIDPattern                 = regexp.MustCompile(`^call_[A-Za-z0-9._:-]{6,96}$`)
	remoteHandlePattern           = regexp.MustCompile(`^[+A-Za-z0-9][A-Za-z0-9 ._()+-]{0,63}$`)
)

type Clock func() time.Time

type SnapshotProvider interface {
	Snapshot() StatusSnapshot
}

type CommandHandler interface {
	HandleCommand(context.Context, Command) error
}

type EventPublisher interface {
	Publish(context.Context, any) error
}

type SessionTransport interface {
	Send(context.Context, []byte) error
	Receive(context.Context) ([]byte, error)
	Close() error
}

type Options struct {
	BridgeID         string
	BridgeVersion    string
	Clock            Clock
	SnapshotProvider SnapshotProvider
	CommandHandler   CommandHandler
	Capabilities     *Capabilities
}

type Server struct {
	bridgeID         string
	bridgeVersion    string
	clock            Clock
	snapshotProvider SnapshotProvider
	commandHandler   CommandHandler
	capabilities     Capabilities

	sessionsMu sync.RWMutex
	sessions   map[SessionTransport]struct{}
}

func NewServer(opts Options) *Server {
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	bridgeID := opts.BridgeID
	if bridgeID == "" {
		bridgeID = "bridge_local_runtime"
	}
	bridgeVersion := opts.BridgeVersion
	if bridgeVersion == "" {
		bridgeVersion = "0.1.0"
	}
	capabilities := DefaultCapabilities()
	if opts.Capabilities != nil {
		capabilities = *opts.Capabilities
	}
	provider := opts.SnapshotProvider
	if provider == nil {
		provider = StaticSnapshotProvider{SnapshotValue: StatusSnapshot{
			BridgeState: BridgeStateDegraded,
			Registration: RegistrationStatus{
				State:      RegistrationStateUnregistered,
				ReasonCode: "not_configured",
				Message:    "No voice line is registered.",
			},
			ActiveCalls: []CallSummary{},
		}}
	}
	return &Server{
		bridgeID:         bridgeID,
		bridgeVersion:    bridgeVersion,
		clock:            clock,
		snapshotProvider: provider,
		commandHandler:   opts.CommandHandler,
		capabilities:     capabilities,
		sessions:         map[SessionTransport]struct{}{},
	}
}

func (s *Server) Hello() HelloEvent {
	return NewHelloEventWithCapabilities(s.clock(), s.bridgeID, s.bridgeVersion, s.capabilities)
}

func (s *Server) Status() StatusEvent {
	return NewStatusEvent(s.clock(), s.snapshotProvider.Snapshot())
}

func (s *Server) SetCommandHandler(handler CommandHandler) {
	s.commandHandler = handler
}

func (s *Server) ServeSession(ctx context.Context, transport SessionTransport) error {
	defer transport.Close()

	if err := s.sendJSON(ctx, transport, s.Hello()); err != nil {
		return err
	}
	if err := s.sendJSON(ctx, transport, s.Status()); err != nil {
		return err
	}
	s.addSession(transport)
	defer s.removeSession(transport)

	for {
		payload, err := transport.Receive(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return err
		}
		if err := s.HandlePayload(ctx, transport, payload); err != nil {
			return err
		}
	}
}

func (s *Server) HandlePayload(ctx context.Context, transport SessionTransport, payload []byte) error {
	var command Command
	if err := json.Unmarshal(payload, &command); err != nil {
		return s.sendError(ctx, transport, "validation_failed", "Invalid JSON message.", false, false, "")
	}
	if command.ProtocolVersion != Version {
		err := s.sendError(ctx, transport, "unsupported_protocol_version", "Unsupported protocol version: "+command.ProtocolVersion, false, true, command.CommandID)
		if err != nil {
			return err
		}
		return ErrUnsupportedProtocolVersion
	}
	if !commandIDPattern.MatchString(command.CommandID) {
		return s.sendError(ctx, transport, "validation_failed", "Invalid commandId.", false, false, "")
	}
	switch command.Type {
	case "status.get":
		return s.sendJSON(ctx, transport, s.Status())
	case "call.answer":
		if !callIDPattern.MatchString(command.CallID) {
			return s.sendError(ctx, transport, "validation_failed", "Invalid callId.", false, false, command.CommandID)
		}
		return s.dispatchCommand(ctx, transport, command)
	case "call.dial":
		if !remoteHandlePattern.MatchString(command.Remote.Handle) {
			return s.sendError(ctx, transport, "validation_failed", "Invalid remote.handle.", false, false, command.CommandID)
		}
		if command.Audio.AudioFormat() != CanonicalAudioFormat() {
			return s.sendError(ctx, transport, "validation_failed", "Unsupported audio format.", false, false, command.CommandID)
		}
		return s.dispatchCommand(ctx, transport, command)
	case "audio.out":
		if !callIDPattern.MatchString(command.CallID) {
			return s.sendError(ctx, transport, "validation_failed", "Invalid callId.", false, false, command.CommandID)
		}
		if command.Sequence < 0 {
			return s.sendError(ctx, transport, "validation_failed", "Invalid audio sequence.", false, false, command.CommandID)
		}
		if _, err := DecodeCanonicalAudioPayload(command.Audio); err != nil {
			if errors.Is(err, ErrUnsupportedAudioFormat) {
				return s.sendError(ctx, transport, "validation_failed", "Unsupported audio format.", false, false, command.CommandID)
			}
			return s.sendError(ctx, transport, "validation_failed", "Invalid audio payload.", false, false, command.CommandID)
		}
		return s.dispatchCommand(ctx, transport, command)
	case "audio.clear":
		if !callIDPattern.MatchString(command.CallID) {
			return s.sendError(ctx, transport, "validation_failed", "Invalid callId.", false, false, command.CommandID)
		}
		if command.Scope != "queued" {
			return s.sendError(ctx, transport, "validation_failed", "Invalid audio.clear scope.", false, false, command.CommandID)
		}
		if command.Reason != "" && command.Reason != "barge_in" && command.Reason != "user_request" && command.Reason != "call_ending" {
			return s.sendError(ctx, transport, "validation_failed", "Invalid audio.clear reason.", false, false, command.CommandID)
		}
		return s.dispatchCommand(ctx, transport, command)
	case "call.hangup":
		if !callIDPattern.MatchString(command.CallID) {
			return s.sendError(ctx, transport, "validation_failed", "Invalid callId.", false, false, command.CommandID)
		}
		if command.Reason != "" && command.Reason != "user_request" && command.Reason != "completed" && command.Reason != "failed" {
			return s.sendError(ctx, transport, "validation_failed", "Invalid hangup reason.", false, false, command.CommandID)
		}
		return s.dispatchCommand(ctx, transport, command)
	default:
		return s.sendError(ctx, transport, "validation_failed", "Unsupported command type.", false, false, command.CommandID)
	}
}

func (s *Server) Publish(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	s.sessionsMu.RLock()
	transports := make([]SessionTransport, 0, len(s.sessions))
	for transport := range s.sessions {
		transports = append(transports, transport)
	}
	s.sessionsMu.RUnlock()

	var sendErr error
	for _, transport := range transports {
		if err := transport.Send(ctx, payload); err != nil {
			s.removeSession(transport)
			sendErr = errors.Join(sendErr, err)
		}
	}
	return sendErr
}

func (s *Server) dispatchCommand(ctx context.Context, transport SessionTransport, command Command) error {
	if s.commandHandler == nil {
		return s.sendErrorWithCall(ctx, transport, "bridge_unavailable", "Bridge call control is not available.", true, false, command.CommandID, command.CallID)
	}
	if err := s.commandHandler.HandleCommand(ctx, command); err != nil {
		return s.sendCommandError(ctx, transport, command, err)
	}
	return nil
}

func (s *Server) addSession(transport SessionTransport) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	s.sessions[transport] = struct{}{}
}

func (s *Server) removeSession(transport SessionTransport) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()

	delete(s.sessions, transport)
}

func (s *Server) sendCommandError(ctx context.Context, transport SessionTransport, command Command, err error) error {
	var commandErr *CommandError
	if errors.As(err, &commandErr) {
		callID := commandErr.CallID
		if callID == "" {
			callID = command.CallID
		}
		code := safeErrorCode(commandErr.Code)
		message := safeErrorMessage(commandErr.Message, "Bridge command failed.")
		if code == "internal_error" && commandErr.Code != "internal_error" {
			message = "Bridge command failed."
		}
		return s.sendErrorWithCall(ctx, transport, code, message, commandErr.Retryable, false, command.CommandID, callID)
	}
	return s.sendErrorWithCall(ctx, transport, "internal_error", "Bridge command failed.", true, false, command.CommandID, command.CallID)
}

func (s *Server) sendError(ctx context.Context, transport SessionTransport, code string, message string, retryable bool, fatal bool, commandID string) error {
	return s.sendJSON(ctx, transport, NewErrorEvent(s.clock(), code, message, retryable, fatal, commandID))
}

func (s *Server) sendErrorWithCall(ctx context.Context, transport SessionTransport, code string, message string, retryable bool, fatal bool, commandID string, callID string) error {
	event := NewErrorEvent(s.clock(), code, message, retryable, fatal, commandID)
	if callIDPattern.MatchString(callID) {
		event.Error.CallID = callID
	}
	return s.sendJSON(ctx, transport, event)
}

func (s *Server) sendJSON(ctx context.Context, transport SessionTransport, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return transport.Send(ctx, payload)
}

type CommandError struct {
	Code      string
	Message   string
	Retryable bool
	CallID    string
}

func (e *CommandError) Error() string {
	return e.Code + ": " + e.Message
}

func NewCommandError(code string, message string, retryable bool, callID string) *CommandError {
	return &CommandError{
		Code:      code,
		Message:   message,
		Retryable: retryable,
		CallID:    callID,
	}
}

type StaticSnapshotProvider struct {
	SnapshotValue StatusSnapshot
}

func (p StaticSnapshotProvider) Snapshot() StatusSnapshot {
	return p.SnapshotValue
}

func safeErrorCode(code string) string {
	switch code {
	case "unsupported_protocol_version", "validation_failed", "bridge_unavailable", "call_not_found", "call_rejected", "audio_underrun", "internal_error":
		return code
	default:
		return "internal_error"
	}
}

func safeErrorMessage(value string, fallback string) string {
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
	message := strings.TrimSpace(string(cleaned))
	if message == "" {
		return fallback
	}
	return message
}
