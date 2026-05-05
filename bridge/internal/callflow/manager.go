package callflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	bridgemedia "github.com/jtcressy/openclaw-sip-voice/bridge/internal/media"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
)

const (
	outcomeCompleted = "completed"
	outcomeError     = "error"
	outcomeRejected  = "rejected"
	outcomeCanceled  = "canceled"
)

type Clock func() time.Time

type State interface {
	Snapshot() protocol.StatusSnapshot
	UpsertCall(protocol.CallSummary)
	RemoveCall(callID string)
}

type EventSink interface {
	Publish(context.Context, any) error
}

type InboundLeg interface {
	Ringing() error
	Answer() error
	Hangup(context.Context) error
}

type OutboundLeg interface {
	Hangup(context.Context) error
}

type OutboundDialer interface {
	Dial(context.Context, OutboundRequest) (OutboundLeg, error)
}

type MediaEndpointProvider interface {
	MediaEndpoints(context.Context) (bridgemedia.Endpoints, error)
}

type MediaFactory interface {
	Attach(context.Context, string, bridgemedia.Endpoints) (bridgemedia.SessionHandle, error)
}

type OutboundRequest struct {
	CallID    string
	CommandID string
	Remote    protocol.RemoteParty
	Audio     protocol.AudioFormat
}

type InboundInvite struct {
	Context           context.Context
	Caller            string
	CallerDisplayName string
	Callee            string
	Leg               InboundLeg
}

type Options struct {
	Clock          Clock
	State          State
	EventSink      EventSink
	OutboundDialer OutboundDialer
	MediaFactory   MediaFactory
}

type Manager struct {
	clock          Clock
	state          State
	sink           EventSink
	outboundDialer OutboundDialer
	mediaFactory   MediaFactory

	mu           sync.Mutex
	nextInbound  int
	nextOutbound int
	calls        map[string]*trackedCall
}

type trackedCall struct {
	id          string
	direction   string
	state       string
	remote      protocol.RemoteParty
	callee      string
	startedAt   time.Time
	done        chan struct{}
	inboundLeg  InboundLeg
	outboundLeg OutboundLeg
	media       bridgemedia.SessionHandle
	mediaErr    error
	ended       bool
}

func NewManager(opts Options) *Manager {
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Manager{
		clock:          clock,
		state:          opts.State,
		sink:           opts.EventSink,
		outboundDialer: opts.OutboundDialer,
		mediaFactory:   opts.MediaFactory,
		calls:          map[string]*trackedCall{},
	}
}

func (m *Manager) HandleCommand(ctx context.Context, command protocol.Command) error {
	switch command.Type {
	case "call.answer":
		return m.Answer(ctx, command.CallID)
	case "call.dial":
		return m.Dial(ctx, command)
	case "call.hangup":
		return m.Hangup(ctx, command.CallID, command.Reason)
	case "audio.out":
		return m.AudioOut(ctx, command)
	case "audio.clear":
		return m.ClearAudio(ctx, command.CallID)
	default:
		return protocol.NewCommandError("validation_failed", "Unsupported command type.", false, command.CallID)
	}
}

func (m *Manager) StartInbound(ctx context.Context, invite InboundInvite) (string, error) {
	if invite.Leg == nil {
		return "", errors.New("inbound call leg is not configured")
	}
	if ctx == nil {
		ctx = invite.Context
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := invite.Leg.Ringing(); err != nil {
		return "", errors.New("inbound call could not be prepared")
	}

	callID := m.nextCallID(protocol.CallDirectionInbound)
	remote := protocol.RemoteParty{
		Handle:      sanitizeHandle(invite.Caller, "unknown"),
		DisplayName: sanitizeDisplayName(invite.CallerDisplayName),
	}
	call := &trackedCall{
		id:         callID,
		direction:  protocol.CallDirectionInbound,
		state:      protocol.CallStateRinging,
		remote:     remote,
		callee:     sanitizeHandle(invite.Callee, ""),
		startedAt:  m.clock(),
		done:       make(chan struct{}),
		inboundLeg: invite.Leg,
	}

	m.mu.Lock()
	m.calls[callID] = call
	m.mu.Unlock()

	m.upsertCall(call)
	if err := m.publish(ctx, protocol.NewCallStartEvent(m.clock(), callID, call.direction, call.state, remote, "")); err != nil {
		m.finishCall(context.Background(), callID, outcomeError, &protocol.CallEndReason{
			Code:    "event_publish_failed",
			Message: "Call event could not be delivered.",
		})
		return "", err
	}
	_ = m.publishStatus(ctx)

	go m.watchCallContext(callID, ctx)
	return callID, nil
}

func (m *Manager) Answer(ctx context.Context, callID string) error {
	call, err := m.callForCommand(callID)
	if err != nil {
		return err
	}
	if call.direction != protocol.CallDirectionInbound || call.state != protocol.CallStateRinging {
		return protocol.NewCommandError("call_rejected", "Call cannot be answered from its current state.", false, callID)
	}
	if call.inboundLeg == nil {
		return protocol.NewCommandError("bridge_unavailable", "Inbound call control is not available.", true, callID)
	}

	if err := call.inboundLeg.Answer(); err != nil {
		m.finishCall(ctx, callID, outcomeError, &protocol.CallEndReason{
			Code:    "answer_failed",
			Message: "Call could not be answered.",
		})
		return protocol.NewCommandError("call_rejected", "Call could not be answered.", true, callID)
	}
	m.attachMedia(ctx, callID, call.inboundLeg)
	return m.transitionCall(ctx, callID, protocol.CallStateActive)
}

func (m *Manager) Dial(ctx context.Context, command protocol.Command) error {
	if command.Audio.AudioFormat() != protocol.CanonicalAudioFormat() {
		return protocol.NewCommandError("validation_failed", "Unsupported audio format.", false, "")
	}
	if m.outboundDialer == nil {
		return protocol.NewCommandError("bridge_unavailable", "Outbound call control is not available.", true, "")
	}

	remote := protocol.RemoteParty{
		Handle:      sanitizeHandle(command.Remote.Handle, "unknown"),
		DisplayName: sanitizeDisplayName(command.Remote.DisplayName),
	}
	callID := m.nextCallID(protocol.CallDirectionOutbound)
	call := &trackedCall{
		id:        callID,
		direction: protocol.CallDirectionOutbound,
		state:     protocol.CallStateDialing,
		remote:    remote,
		startedAt: m.clock(),
		done:      make(chan struct{}),
	}

	m.mu.Lock()
	m.calls[callID] = call
	m.mu.Unlock()

	m.upsertCall(call)
	if err := m.publish(ctx, protocol.NewCallStartEvent(m.clock(), callID, call.direction, call.state, remote, command.CommandID)); err != nil {
		m.finishCall(context.Background(), callID, outcomeError, &protocol.CallEndReason{
			Code:    "event_publish_failed",
			Message: "Call event could not be delivered.",
		})
		return err
	}
	_ = m.publishStatus(ctx)

	req := OutboundRequest{
		CallID:    callID,
		CommandID: command.CommandID,
		Remote:    remote,
		Audio:     protocol.CanonicalAudioFormat(),
	}
	go m.completeOutboundDial(callID, req)
	return nil
}

func (m *Manager) Hangup(ctx context.Context, callID string, reason string) error {
	call, err := m.callForCommand(callID)
	if err != nil {
		return err
	}
	if call.state == protocol.CallStateEnding {
		return nil
	}

	previousState := call.state
	if err := m.transitionCall(ctx, callID, protocol.CallStateEnding); err != nil {
		return err
	}

	var hangupErr error
	if call.inboundLeg != nil {
		hangupErr = call.inboundLeg.Hangup(ctx)
	} else if call.outboundLeg != nil {
		hangupErr = call.outboundLeg.Hangup(ctx)
	}
	if hangupErr != nil {
		m.finishCall(ctx, callID, outcomeError, &protocol.CallEndReason{
			Code:    "hangup_failed",
			Message: "Call could not be ended cleanly.",
		})
		return protocol.NewCommandError("call_rejected", "Call could not be ended cleanly.", true, callID)
	}

	outcome := hangupOutcome(call.direction, previousState, reason)
	var endReason *protocol.CallEndReason
	if outcome == outcomeError {
		endReason = &protocol.CallEndReason{
			Code:    "hangup_failed",
			Message: "Call ended with a failure reason.",
		}
	}
	m.finishCall(ctx, callID, outcome, endReason)
	return nil
}

func (m *Manager) AudioOut(ctx context.Context, command protocol.Command) error {
	if command.Sequence < 0 {
		return protocol.NewCommandError("validation_failed", "Invalid audio sequence.", false, command.CallID)
	}
	payload, err := protocol.DecodeCanonicalAudioPayload(command.Audio)
	if err != nil {
		if errors.Is(err, protocol.ErrUnsupportedAudioFormat) {
			return protocol.NewCommandError("validation_failed", "Unsupported audio format.", false, command.CallID)
		}
		return protocol.NewCommandError("validation_failed", "Invalid audio payload.", false, command.CallID)
	}

	session, err := m.mediaForCommand(command.CallID)
	if err != nil {
		return err
	}
	if err := session.Enqueue(ctx, bridgemedia.Frame{
		Sequence: command.Sequence,
		Payload:  payload,
	}); err != nil {
		switch {
		case errors.Is(err, bridgemedia.ErrInvalidFrame):
			return protocol.NewCommandError("validation_failed", "Invalid audio payload.", false, command.CallID)
		case errors.Is(err, bridgemedia.ErrQueueFull):
			return protocol.NewCommandError("call_rejected", "Outbound audio queue is full.", false, command.CallID)
		default:
			return protocol.NewCommandError("bridge_unavailable", "Call media is not available.", true, command.CallID)
		}
	}
	return nil
}

func (m *Manager) ClearAudio(_ context.Context, callID string) error {
	session, err := m.mediaForCommand(callID)
	if err != nil {
		return err
	}
	session.ClearOutbound()
	return nil
}

func (m *Manager) Wait(callID string) {
	m.mu.Lock()
	call := m.calls[callID]
	if call == nil {
		m.mu.Unlock()
		return
	}
	done := call.done
	m.mu.Unlock()

	<-done
}

func (m *Manager) completeOutboundDial(callID string, req OutboundRequest) {
	leg, err := m.outboundDialer.Dial(context.Background(), req)
	if err != nil {
		m.finishCall(context.Background(), callID, outcomeError, &protocol.CallEndReason{
			Code:    "dial_failed",
			Message: "Outbound call could not be completed.",
		})
		return
	}

	m.mu.Lock()
	call := m.calls[callID]
	if call == nil || call.ended {
		m.mu.Unlock()
		if leg != nil {
			_ = leg.Hangup(context.Background())
		}
		return
	}
	call.outboundLeg = leg
	if call.state != protocol.CallStateEnding {
		call.state = protocol.CallStateActive
	}
	summary := call.summary()
	m.mu.Unlock()

	if summary.State == protocol.CallStateActive {
		m.attachMedia(context.Background(), callID, leg)
		if m.state != nil {
			m.state.UpsertCall(summary)
		}
		_ = m.publishStatus(context.Background())
	}
}

func (m *Manager) watchCallContext(callID string, ctx context.Context) {
	m.mu.Lock()
	call := m.calls[callID]
	if call == nil {
		m.mu.Unlock()
		return
	}
	done := call.done
	m.mu.Unlock()

	select {
	case <-ctx.Done():
		m.finishRemoteEnded(callID)
	case <-done:
	}
}

func (m *Manager) finishRemoteEnded(callID string) {
	m.mu.Lock()
	call := m.calls[callID]
	if call == nil || call.ended {
		m.mu.Unlock()
		return
	}
	state := call.state
	direction := call.direction
	m.mu.Unlock()

	outcome := outcomeCompleted
	if state == protocol.CallStateRinging || state == protocol.CallStateDialing {
		outcome = outcomeCanceled
	}
	if direction == protocol.CallDirectionInbound && state == protocol.CallStateRinging {
		outcome = outcomeCanceled
	}
	m.finishCall(context.Background(), callID, outcome, nil)
}

func (m *Manager) transitionCall(ctx context.Context, callID string, state string) error {
	m.mu.Lock()
	call := m.calls[callID]
	if call == nil || call.ended {
		m.mu.Unlock()
		return protocol.NewCommandError("call_not_found", "Call was not found.", false, callID)
	}
	call.state = state
	summary := call.summary()
	m.mu.Unlock()

	if m.state != nil {
		m.state.UpsertCall(summary)
	}
	return m.publishStatus(ctx)
}

func (m *Manager) finishCall(ctx context.Context, callID string, outcome string, reason *protocol.CallEndReason) {
	m.mu.Lock()
	call := m.calls[callID]
	if call == nil || call.ended {
		m.mu.Unlock()
		return
	}
	call.ended = true
	delete(m.calls, callID)
	durationMs := durationMillis(m.clock().Sub(call.startedAt))
	done := call.done
	mediaSession := call.media
	m.mu.Unlock()

	if mediaSession != nil {
		_ = mediaSession.Close()
	}
	if m.state != nil {
		m.state.RemoveCall(callID)
	}
	_ = m.publish(ctx, protocol.NewCallEndEvent(m.clock(), callID, outcome, durationMs, reason))
	_ = m.publishStatus(ctx)
	close(done)
}

func (m *Manager) callForCommand(callID string) (*trackedCall, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	call := m.calls[callID]
	if call == nil || call.ended {
		return nil, protocol.NewCommandError("call_not_found", "Call was not found.", false, callID)
	}
	return call, nil
}

func (m *Manager) upsertCall(call *trackedCall) {
	if m.state == nil || call == nil {
		return
	}
	m.state.UpsertCall(call.summary())
}

func (m *Manager) publishStatus(ctx context.Context) error {
	if m.state == nil {
		return nil
	}
	return m.publish(ctx, protocol.NewStatusEvent(m.clock(), m.state.Snapshot()))
}

func (m *Manager) publish(ctx context.Context, event any) error {
	if m.sink == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return m.sink.Publish(ctx, event)
}

func (m *Manager) attachMedia(ctx context.Context, callID string, source any) {
	if m.mediaFactory == nil {
		return
	}
	provider, ok := source.(MediaEndpointProvider)
	if !ok {
		return
	}
	endpoints, err := provider.MediaEndpoints(ctx)
	if err != nil {
		m.setMediaError(callID, err)
		return
	}
	session, err := m.mediaFactory.Attach(ctx, callID, endpoints)
	if err != nil {
		m.setMediaError(callID, err)
		return
	}

	var old bridgemedia.SessionHandle
	m.mu.Lock()
	call := m.calls[callID]
	if call == nil || call.ended {
		m.mu.Unlock()
		_ = session.Close()
		return
	}
	old = call.media
	call.media = session
	call.mediaErr = nil
	m.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
}

func (m *Manager) setMediaError(callID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	call := m.calls[callID]
	if call == nil || call.ended {
		return
	}
	call.mediaErr = err
}

func (m *Manager) mediaForCommand(callID string) (bridgemedia.SessionHandle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	call := m.calls[callID]
	if call == nil || call.ended {
		return nil, protocol.NewCommandError("call_not_found", "Call was not found.", false, callID)
	}
	if call.state != protocol.CallStateActive {
		return nil, protocol.NewCommandError("call_rejected", "Call media is not active.", false, callID)
	}
	if errors.Is(call.mediaErr, bridgemedia.ErrUnsupportedCodec) {
		return nil, protocol.NewCommandError("call_rejected", "Call media codec is not supported.", false, callID)
	}
	if call.mediaErr != nil || call.media == nil {
		return nil, protocol.NewCommandError("bridge_unavailable", "Call media is not available.", true, callID)
	}
	return call.media, nil
}

func (m *Manager) nextCallID(direction string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch direction {
	case protocol.CallDirectionInbound:
		m.nextInbound++
		return fmt.Sprintf("call_in_%06d", m.nextInbound)
	case protocol.CallDirectionOutbound:
		m.nextOutbound++
		return fmt.Sprintf("call_out_%06d", m.nextOutbound)
	default:
		m.nextOutbound++
		return fmt.Sprintf("call_out_%06d", m.nextOutbound)
	}
}

func (c *trackedCall) summary() protocol.CallSummary {
	return protocol.CallSummary{
		CallID:    c.id,
		Direction: c.direction,
		State:     c.state,
	}
}

func hangupOutcome(direction string, state string, reason string) string {
	if reason == "failed" {
		return outcomeError
	}
	if direction == protocol.CallDirectionInbound && state == protocol.CallStateRinging {
		return outcomeRejected
	}
	if state == protocol.CallStateDialing {
		return outcomeCanceled
	}
	return outcomeCompleted
}

func durationMillis(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	return int(duration / time.Millisecond)
}

func sanitizeHandle(value string, fallback string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'<>")
	if strings.HasPrefix(strings.ToLower(value), "sip:") {
		value = value[4:]
	}
	if before, _, ok := strings.Cut(value, "@"); ok {
		value = before
	}
	if strings.HasPrefix(strings.ToLower(value), "sip:") {
		value = value[4:]
	}

	cleaned := make([]rune, 0, len(value))
	for _, r := range value {
		if len(cleaned) == 0 && !(r == '+' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._()+-", r) {
			cleaned = append(cleaned, r)
		}
		if len(cleaned) >= 64 {
			break
		}
	}
	result := strings.TrimSpace(string(cleaned))
	if result == "" {
		result = fallback
	}
	if result == "" {
		result = "unknown"
	}
	return result
}

func sanitizeDisplayName(value string) string {
	cleaned := make([]rune, 0, len(value))
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsControl(r) {
			if len(cleaned) == 0 || cleaned[len(cleaned)-1] != ' ' {
				cleaned = append(cleaned, ' ')
			}
			continue
		}
		cleaned = append(cleaned, r)
		if len(cleaned) >= 80 {
			break
		}
	}
	return strings.TrimSpace(string(cleaned))
}
