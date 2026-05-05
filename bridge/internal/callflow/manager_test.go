package callflow

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
	bridgemedia "github.com/jtcressy/openclaw-sip-voice/bridge/internal/media"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
	bridgeruntime "github.com/jtcressy/openclaw-sip-voice/bridge/internal/runtime"
)

func TestInboundCallAnswerAndHangupDriveState(t *testing.T) {
	state := testState(t)
	sink := &recordingSink{}
	leg := &fakeInboundLeg{}
	manager := NewManager(Options{
		Clock:     fixedTestClock(),
		State:     state,
		EventSink: sink,
	})

	callID, err := manager.StartInbound(context.Background(), InboundInvite{
		Caller:            "sip:+15551234567@198.51.100.10",
		CallerDisplayName: "Front\nDesk",
		Callee:            "sip:1234@198.51.100.10",
		Leg:               leg,
	})
	if err != nil {
		t.Fatalf("start inbound: %v", err)
	}
	if callID != "call_in_000001" {
		t.Fatalf("callID = %q, want call_in_000001", callID)
	}
	if leg.ringingCalls != 1 {
		t.Fatalf("ringing calls = %d, want 1", leg.ringingCalls)
	}

	start := sink.firstCallStart(t)
	if start.Direction != protocol.CallDirectionInbound || start.State != protocol.CallStateRinging {
		t.Fatalf("call.start direction/state = %s/%s, want inbound/ringing", start.Direction, start.State)
	}
	if start.Remote.Handle != "+15551234567" {
		t.Fatalf("remote handle = %q, want sanitized caller", start.Remote.Handle)
	}
	if strings.Contains(start.Remote.Handle, "198.51.100.10") {
		t.Fatalf("remote handle leaked SIP host: %q", start.Remote.Handle)
	}
	if start.Remote.DisplayName != "Front Desk" {
		t.Fatalf("display name = %q, want sanitized display name", start.Remote.DisplayName)
	}
	assertActiveCallState(t, state, callID, protocol.CallStateRinging)

	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.answer",
		CommandID: "cmd_answer_000001",
		CallID:    callID,
	}); err != nil {
		t.Fatalf("answer call: %v", err)
	}
	if leg.answerCalls != 1 {
		t.Fatalf("answer calls = %d, want 1", leg.answerCalls)
	}
	assertActiveCallState(t, state, callID, protocol.CallStateActive)

	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.hangup",
		CommandID: "cmd_hangup_000001",
		CallID:    callID,
		Reason:    "user_request",
	}); err != nil {
		t.Fatalf("hangup call: %v", err)
	}
	if leg.hangupCalls != 1 {
		t.Fatalf("hangup calls = %d, want 1", leg.hangupCalls)
	}
	assertNoActiveCalls(t, state)

	end := sink.lastCallEnd(t)
	if end.CallID != callID || end.Outcome != outcomeCompleted {
		t.Fatalf("call.end = %+v, want completed for %s", end, callID)
	}
}

func TestInboundRemoteCancelEndsRingingCall(t *testing.T) {
	state := testState(t)
	sink := &recordingSink{}
	leg := &fakeInboundLeg{}
	manager := NewManager(Options{
		Clock:     fixedTestClock(),
		State:     state,
		EventSink: sink,
	})
	ctx, cancel := context.WithCancel(context.Background())

	callID, err := manager.StartInbound(ctx, InboundInvite{
		Caller: "sip:+15551234567@198.51.100.10",
		Callee: "1234",
		Leg:    leg,
	})
	if err != nil {
		t.Fatalf("start inbound: %v", err)
	}
	cancel()
	waitFor(t, func() bool {
		return sink.callEndCount() > 0
	})

	assertNoActiveCalls(t, state)
	end := sink.lastCallEnd(t)
	if end.CallID != callID {
		t.Fatalf("call.end callID = %q, want %s", end.CallID, callID)
	}
	if end.Outcome != outcomeCanceled {
		t.Fatalf("call.end outcome = %q, want canceled", end.Outcome)
	}
	if leg.hangupCalls != 0 {
		t.Fatalf("hangup calls = %d, want 0 for remote cancel", leg.hangupCalls)
	}
}

func TestOutboundDialTracksPendingThenActiveAndHangup(t *testing.T) {
	state := testState(t)
	sink := &recordingSink{}
	outboundLeg := &fakeOutboundLeg{}
	dialer := newBlockingDialer(outboundLeg, nil)
	manager := NewManager(Options{
		Clock:          fixedTestClock(),
		State:          state,
		EventSink:      sink,
		OutboundDialer: dialer,
	})

	err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.dial",
		CommandID: "cmd_dial_000001",
		Remote: protocol.RemoteParty{
			Handle:      "+15557654321",
			DisplayName: "Support Line",
		},
		Audio: protocol.NewAudioFrame(""),
	})
	if err != nil {
		t.Fatalf("dial call: %v", err)
	}

	req := dialer.waitStarted(t)
	if req.CallID != "call_out_000001" {
		t.Fatalf("outbound callID = %q, want call_out_000001", req.CallID)
	}
	assertActiveCallState(t, state, req.CallID, protocol.CallStateDialing)
	start := sink.firstCallStart(t)
	if start.RequestedByCommandID != "cmd_dial_000001" {
		t.Fatalf("requestedByCommandId = %q, want cmd_dial_000001", start.RequestedByCommandID)
	}

	dialer.release()
	waitFor(t, func() bool {
		return activeCallState(state, req.CallID) == protocol.CallStateActive
	})

	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.hangup",
		CommandID: "cmd_hangup_000001",
		CallID:    req.CallID,
		Reason:    "completed",
	}); err != nil {
		t.Fatalf("hangup outbound call: %v", err)
	}
	if outboundLeg.hangupCalls != 1 {
		t.Fatalf("outbound hangup calls = %d, want 1", outboundLeg.hangupCalls)
	}
	assertNoActiveCalls(t, state)
	if end := sink.lastCallEnd(t); end.Outcome != outcomeCompleted {
		t.Fatalf("call.end outcome = %q, want completed", end.Outcome)
	}
}

func TestOutboundDialFailureEmitsSanitizedCallEnd(t *testing.T) {
	state := testState(t)
	sink := &recordingSink{}
	dialer := newBlockingDialer(nil, errors.New("dial failed for alice super-secret at 198.51.100.10"))
	manager := NewManager(Options{
		Clock:          fixedTestClock(),
		State:          state,
		EventSink:      sink,
		OutboundDialer: dialer,
	})

	err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.dial",
		CommandID: "cmd_dial_000001",
		Remote: protocol.RemoteParty{
			Handle: "+15557654321",
		},
		Audio: protocol.NewAudioFrame(""),
	})
	if err != nil {
		t.Fatalf("dial call: %v", err)
	}
	req := dialer.waitStarted(t)
	dialer.release()
	waitFor(t, func() bool {
		return sink.callEndCount() > 0
	})

	end := sink.lastCallEnd(t)
	if end.CallID != req.CallID || end.Outcome != outcomeError {
		t.Fatalf("call.end = %+v, want outbound error for %s", end, req.CallID)
	}
	if end.Reason == nil || end.Reason.Code != "dial_failed" {
		t.Fatalf("call.end reason = %+v, want dial_failed", end.Reason)
	}
	assertNoCredentialLeak(t, end.Reason.Message)
}

func TestInboundAudioOutAndClearUseAttachedMedia(t *testing.T) {
	state := testState(t)
	session := &fakeMediaSession{}
	factory := &fakeMediaFactory{session: session}
	leg := &fakeMediaInboundLeg{
		endpoints: bridgemedia.Endpoints{
			Reader: bytes.NewReader(nil),
			Writer: &bytes.Buffer{},
			Codec:  bridgemedia.CodecPCMU,
		},
	}
	manager := NewManager(Options{
		Clock:        fixedTestClock(),
		State:        state,
		EventSink:    &recordingSink{},
		MediaFactory: factory,
	})

	callID, err := manager.StartInbound(context.Background(), InboundInvite{
		Caller: "sip:+15551234567@198.51.100.10",
		Callee: "1234",
		Leg:    leg,
	})
	if err != nil {
		t.Fatalf("start inbound: %v", err)
	}
	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.answer",
		CommandID: "cmd_answer_000001",
		CallID:    callID,
	}); err != nil {
		t.Fatalf("answer call: %v", err)
	}
	if got := factory.callIDValue(); got != callID {
		t.Fatalf("attached callID = %q, want %s", got, callID)
	}

	payload := bytes.Repeat([]byte{0xff}, bridgemedia.FrameBytes)
	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "audio.out",
		CommandID: "cmd_audio_000001",
		CallID:    callID,
		Sequence:  7,
		Audio:     protocol.NewAudioFrameFromPayload(payload),
	}); err != nil {
		t.Fatalf("audio.out: %v", err)
	}
	if got := session.frameCount(); got != 1 {
		t.Fatalf("queued frames = %d, want 1", got)
	}

	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "audio.clear",
		CommandID: "cmd_clear_000001",
		CallID:    callID,
		Scope:     "queued",
	}); err != nil {
		t.Fatalf("audio.clear: %v", err)
	}
	if session.clearCalls != 1 {
		t.Fatalf("clear calls = %d, want 1", session.clearCalls)
	}

	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.hangup",
		CommandID: "cmd_hangup_000001",
		CallID:    callID,
		Reason:    "completed",
	}); err != nil {
		t.Fatalf("hangup call: %v", err)
	}
	if session.closeCalls != 1 {
		t.Fatalf("media close calls = %d, want 1", session.closeCalls)
	}
}

func TestOutboundDialAttachesMediaWhenActive(t *testing.T) {
	state := testState(t)
	session := &fakeMediaSession{}
	factory := &fakeMediaFactory{session: session}
	outboundLeg := &fakeMediaOutboundLeg{
		fakeOutboundLeg: &fakeOutboundLeg{},
		endpoints: bridgemedia.Endpoints{
			Reader: bytes.NewReader(nil),
			Writer: &bytes.Buffer{},
			Codec:  bridgemedia.CodecPCMU,
		},
	}
	dialer := newBlockingDialer(outboundLeg, nil)
	manager := NewManager(Options{
		Clock:          fixedTestClock(),
		State:          state,
		EventSink:      &recordingSink{},
		OutboundDialer: dialer,
		MediaFactory:   factory,
	})

	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.dial",
		CommandID: "cmd_dial_000001",
		Remote: protocol.RemoteParty{
			Handle: "+15557654321",
		},
		Audio: protocol.NewAudioFrame(""),
	}); err != nil {
		t.Fatalf("dial call: %v", err)
	}
	req := dialer.waitStarted(t)
	dialer.release()
	waitFor(t, func() bool {
		return factory.callIDValue() == req.CallID
	})

	payload := bytes.Repeat([]byte{0xfe}, bridgemedia.FrameBytes)
	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "audio.out",
		CommandID: "cmd_audio_000001",
		CallID:    req.CallID,
		Sequence:  1,
		Audio:     protocol.NewAudioFrameFromPayload(payload),
	}); err != nil {
		t.Fatalf("audio.out: %v", err)
	}
	if got := session.frameCount(); got != 1 {
		t.Fatalf("queued frames = %d, want 1", got)
	}
}

func TestUnsupportedMediaCodecRejectsAudioCommandsWithoutLeakingAudio(t *testing.T) {
	state := testState(t)
	factory := &fakeMediaFactory{err: bridgemedia.ErrUnsupportedCodec}
	leg := &fakeMediaInboundLeg{
		endpoints: bridgemedia.Endpoints{
			Reader: bytes.NewReader(bytes.Repeat([]byte{0xd5}, bridgemedia.FrameBytes)),
			Writer: &bytes.Buffer{},
			Codec:  bridgemedia.CodecPCMA,
		},
	}
	manager := NewManager(Options{
		Clock:        fixedTestClock(),
		State:        state,
		EventSink:    &recordingSink{},
		MediaFactory: factory,
	})

	callID, err := manager.StartInbound(context.Background(), InboundInvite{
		Caller: "sip:+15551234567@198.51.100.10",
		Callee: "1234",
		Leg:    leg,
	})
	if err != nil {
		t.Fatalf("start inbound: %v", err)
	}
	if err := manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "call.answer",
		CommandID: "cmd_answer_000001",
		CallID:    callID,
	}); err != nil {
		t.Fatalf("answer call: %v", err)
	}

	err = manager.HandleCommand(context.Background(), protocol.Command{
		Type:      "audio.out",
		CommandID: "cmd_audio_000001",
		CallID:    callID,
		Sequence:  1,
		Audio:     protocol.NewAudioFrameFromPayload(bytes.Repeat([]byte{0xff}, bridgemedia.FrameBytes)),
	})
	var commandErr *protocol.CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("audio.out error = %v, want CommandError", err)
	}
	if commandErr.Code != "call_rejected" || strings.Contains(commandErr.Message, "PCMA") {
		t.Fatalf("command error = %+v, want sanitized unsupported media rejection", commandErr)
	}
}

func testState(t *testing.T) *bridgeruntime.State {
	t.Helper()
	cfg, err := config.ParseEnv([]string{
		"SIP_BIND_ADDR=127.0.0.1:5060",
		"SIP_ADVERTISE_ADDR=127.0.0.1:5060",
	})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return bridgeruntime.NewState(cfg)
}

func fixedTestClock() Clock {
	base := time.Date(2026, 5, 5, 17, 0, 0, 0, time.UTC)
	return func() time.Time {
		return base
	}
}

func assertActiveCallState(t *testing.T, state *bridgeruntime.State, callID string, want string) {
	t.Helper()
	if got := activeCallState(state, callID); got != want {
		t.Fatalf("active call %s state = %q, want %q", callID, got, want)
	}
}

func activeCallState(state *bridgeruntime.State, callID string) string {
	for _, call := range state.Snapshot().ActiveCalls {
		if call.CallID == callID {
			return call.State
		}
	}
	return ""
}

func assertNoActiveCalls(t *testing.T, state *bridgeruntime.State) {
	t.Helper()
	if got := len(state.Snapshot().ActiveCalls); got != 0 {
		t.Fatalf("active calls = %d, want 0", got)
	}
}

func assertNoCredentialLeak(t *testing.T, value string) {
	t.Helper()
	for _, forbidden := range []string{"198.51.100.10", "alice", "super-secret", "1234"} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("value leaked %q: %s", forbidden, value)
		}
	}
}

func waitFor(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("condition was not reached")
		case <-tick.C:
		}
	}
}

type recordingSink struct {
	mu     sync.Mutex
	events []any
}

func (s *recordingSink) Publish(_ context.Context, event any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *recordingSink) firstCallStart(t *testing.T) protocol.CallStartEvent {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range s.events {
		if start, ok := event.(protocol.CallStartEvent); ok {
			return start
		}
	}
	t.Fatal("no call.start event recorded")
	return protocol.CallStartEvent{}
}

func (s *recordingSink) lastCallEnd(t *testing.T) protocol.CallEndEvent {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if end, ok := s.events[i].(protocol.CallEndEvent); ok {
			return end
		}
	}
	t.Fatal("no call.end event recorded")
	return protocol.CallEndEvent{}
}

func (s *recordingSink) callEndCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, event := range s.events {
		if _, ok := event.(protocol.CallEndEvent); ok {
			count++
		}
	}
	return count
}

type fakeInboundLeg struct {
	ringingCalls int
	answerCalls  int
	hangupCalls  int
}

func (l *fakeInboundLeg) Ringing() error {
	l.ringingCalls++
	return nil
}

func (l *fakeInboundLeg) Answer() error {
	l.answerCalls++
	return nil
}

func (l *fakeInboundLeg) Hangup(context.Context) error {
	l.hangupCalls++
	return nil
}

type fakeOutboundLeg struct {
	hangupCalls int
}

func (l *fakeOutboundLeg) Hangup(context.Context) error {
	l.hangupCalls++
	return nil
}

type fakeMediaInboundLeg struct {
	fakeInboundLeg
	endpoints bridgemedia.Endpoints
}

func (l *fakeMediaInboundLeg) MediaEndpoints(context.Context) (bridgemedia.Endpoints, error) {
	return l.endpoints, nil
}

type fakeMediaOutboundLeg struct {
	*fakeOutboundLeg
	endpoints bridgemedia.Endpoints
}

func (l *fakeMediaOutboundLeg) MediaEndpoints(context.Context) (bridgemedia.Endpoints, error) {
	return l.endpoints, nil
}

type fakeMediaFactory struct {
	mu        sync.Mutex
	callID    string
	endpoints bridgemedia.Endpoints
	session   *fakeMediaSession
	err       error
}

func (f *fakeMediaFactory) Attach(_ context.Context, callID string, endpoints bridgemedia.Endpoints) (bridgemedia.SessionHandle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callID = callID
	f.endpoints = endpoints
	if f.err != nil {
		return nil, f.err
	}
	if f.session == nil {
		f.session = &fakeMediaSession{}
	}
	return f.session, nil
}

func (f *fakeMediaFactory) callIDValue() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callID
}

type fakeMediaSession struct {
	mu         sync.Mutex
	frames     []bridgemedia.Frame
	clearCalls int
	closeCalls int
	err        error
}

func (s *fakeMediaSession) Enqueue(_ context.Context, frame bridgemedia.Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	frame.Payload = append([]byte(nil), frame.Payload...)
	s.frames = append(s.frames, frame)
	return nil
}

func (s *fakeMediaSession) ClearOutbound() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := len(s.frames)
	s.frames = nil
	s.clearCalls++
	return dropped
}

func (s *fakeMediaSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	return nil
}

func (s *fakeMediaSession) frameCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.frames)
}

type blockingDialer struct {
	started chan OutboundRequest
	done    chan struct{}
	leg     OutboundLeg
	err     error
}

func newBlockingDialer(leg OutboundLeg, err error) *blockingDialer {
	return &blockingDialer{
		started: make(chan OutboundRequest, 1),
		done:    make(chan struct{}),
		leg:     leg,
		err:     err,
	}
}

func (d *blockingDialer) Dial(_ context.Context, req OutboundRequest) (OutboundLeg, error) {
	d.started <- req
	<-d.done
	return d.leg, d.err
}

func (d *blockingDialer) waitStarted(t *testing.T) OutboundRequest {
	t.Helper()
	select {
	case req := <-d.started:
		return req
	case <-time.After(time.Second):
		t.Fatal("dialer was not called")
	}
	return OutboundRequest{}
}

func (d *blockingDialer) release() {
	close(d.done)
}
