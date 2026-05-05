package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestHelloMatchesProtocolFixtureShape(t *testing.T) {
	server := NewServer(Options{
		BridgeID:      "bridge_loopback_poc",
		BridgeVersion: "0.1.0",
		Clock:         fixedClock("2026-05-05T17:00:00Z"),
	})

	assertJSONEqualFixture(t, server.Hello(), "hello.valid.json")
}

func TestStatusMatchesProtocolFixtureShape(t *testing.T) {
	server := NewServer(Options{
		Clock: fixedClock("2026-05-05T17:00:02Z"),
		SnapshotProvider: StaticSnapshotProvider{SnapshotValue: StatusSnapshot{
			BridgeState: BridgeStateDegraded,
			Registration: RegistrationStatus{
				State:      RegistrationStateUnregistered,
				ReasonCode: "not_configured",
				Message:    "No voice line is registered.",
			},
			ActiveCalls: []CallSummary{},
		}},
	})

	assertJSONEqualFixture(t, server.Status(), "status.unregistered.valid.json")
}

func TestServeSessionSendsHelloStatusAndAcceptsStatusGet(t *testing.T) {
	command := readFixture(t, "status-get.valid.json")
	transport := &fakeTransport{incoming: [][]byte{command}}

	server := NewServer(Options{
		BridgeID:      "bridge_loopback_poc",
		BridgeVersion: "0.1.0",
		Clock:         fixedClock("2026-05-05T17:00:00Z"),
		SnapshotProvider: StaticSnapshotProvider{SnapshotValue: StatusSnapshot{
			BridgeState: BridgeStateDegraded,
			Registration: RegistrationStatus{
				State:      RegistrationStateUnregistered,
				ReasonCode: "not_configured",
				Message:    "No voice line is registered.",
			},
			ActiveCalls: []CallSummary{},
		}},
	})

	if err := server.ServeSession(context.Background(), transport); err != nil {
		t.Fatalf("serve session: %v", err)
	}
	if len(transport.sent) != 3 {
		t.Fatalf("sent messages = %d, want 3", len(transport.sent))
	}

	assertMessageType(t, transport.sent[0], "hello")
	assertMessageType(t, transport.sent[1], "status")
	assertMessageType(t, transport.sent[2], "status")
	if !transport.closed {
		t.Fatal("transport was not closed")
	}
}

func TestUnsupportedProtocolVersionReturnsFatalError(t *testing.T) {
	transport := &fakeTransport{}
	server := NewServer(Options{Clock: fixedClock("2026-05-05T17:00:00Z")})

	err := server.HandlePayload(context.Background(), transport, []byte(`{"protocolVersion":"9.9","type":"status.get","sentAt":"2026-05-05T17:00:11.000Z","commandId":"cmd_status_000001"}`))
	if !errors.Is(err, ErrUnsupportedProtocolVersion) {
		t.Fatalf("error = %v, want ErrUnsupportedProtocolVersion", err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(transport.sent))
	}

	var event ErrorEvent
	if err := json.Unmarshal(transport.sent[0], &event); err != nil {
		t.Fatalf("unmarshal error event: %v", err)
	}
	if event.Error.Code != "unsupported_protocol_version" || !event.Fatal {
		t.Fatalf("error event = %+v, want fatal unsupported_protocol_version", event)
	}
	if !reflect.DeepEqual(event.Error.ExpectedProtocolVersions, []string{Version}) {
		t.Fatalf("expected versions = %#v, want [%q]", event.Error.ExpectedProtocolVersions, Version)
	}
}

func TestHandlePayloadDispatchesCallDialCommand(t *testing.T) {
	transport := &fakeTransport{}
	handler := &fakeCommandHandler{}
	server := NewServer(Options{
		Clock:          fixedClock("2026-05-05T17:00:00Z"),
		CommandHandler: handler,
	})

	if err := server.HandlePayload(context.Background(), transport, readFixture(t, "call-dial.valid.json")); err != nil {
		t.Fatalf("handle call.dial: %v", err)
	}
	if handler.calls != 1 {
		t.Fatalf("handler calls = %d, want 1", handler.calls)
	}
	if handler.command.Type != "call.dial" {
		t.Fatalf("command type = %q, want call.dial", handler.command.Type)
	}
	if handler.command.Remote.Handle != "+15557654321" {
		t.Fatalf("remote handle = %q, want +15557654321", handler.command.Remote.Handle)
	}
	if handler.command.Audio.AudioFormat() != CanonicalAudioFormat() {
		t.Fatalf("audio = %+v, want canonical", handler.command.Audio)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("sent messages = %d, want 0", len(transport.sent))
	}
}

func TestHandlePayloadDispatchesAudioOutCommand(t *testing.T) {
	transport := &fakeTransport{}
	handler := &fakeCommandHandler{}
	server := NewServer(Options{
		Clock:          fixedClock("2026-05-05T17:00:00Z"),
		CommandHandler: handler,
	})

	if err := server.HandlePayload(context.Background(), transport, readFixture(t, "audio-out.valid.json")); err != nil {
		t.Fatalf("handle audio.out: %v", err)
	}
	if handler.calls != 1 {
		t.Fatalf("handler calls = %d, want 1", handler.calls)
	}
	if handler.command.Type != "audio.out" {
		t.Fatalf("command type = %q, want audio.out", handler.command.Type)
	}
	if handler.command.Sequence != 43 {
		t.Fatalf("sequence = %d, want 43", handler.command.Sequence)
	}
	if handler.command.Audio.AudioFormat() != CanonicalAudioFormat() {
		t.Fatalf("audio = %+v, want canonical", handler.command.Audio)
	}
	if _, err := DecodeCanonicalAudioPayload(handler.command.Audio); err != nil {
		t.Fatalf("decode audio payload: %v", err)
	}
	if len(transport.sent) != 0 {
		t.Fatalf("sent messages = %d, want 0", len(transport.sent))
	}
}

func TestHandlePayloadRejectsInvalidAudioOutPayload(t *testing.T) {
	transport := &fakeTransport{}
	handler := &fakeCommandHandler{}
	server := NewServer(Options{
		Clock:          fixedClock("2026-05-05T17:00:00Z"),
		CommandHandler: handler,
	})

	err := server.HandlePayload(context.Background(), transport, []byte(`{"protocolVersion":"1.0","type":"audio.out","sentAt":"2026-05-05T17:00:06.000Z","commandId":"cmd_audio_000001","callId":"call_in_000001","sequence":43,"audio":{"format":"g711_ulaw","sampleRateHz":8000,"channels":1,"frameDurationMs":20,"payloadEncoding":"base64","payload":"not-base64"}}`))
	if err != nil {
		t.Fatalf("handle invalid audio.out: %v", err)
	}
	if handler.calls != 0 {
		t.Fatalf("handler calls = %d, want 0", handler.calls)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(transport.sent))
	}
	var event ErrorEvent
	if err := json.Unmarshal(transport.sent[0], &event); err != nil {
		t.Fatalf("unmarshal error event: %v", err)
	}
	if event.Error.Code != "validation_failed" || event.Error.CommandID != "cmd_audio_000001" {
		t.Fatalf("error event = %+v, want validation_failed for audio command", event)
	}
}

func TestHandlePayloadDispatchesAudioClearCommand(t *testing.T) {
	transport := &fakeTransport{}
	handler := &fakeCommandHandler{}
	server := NewServer(Options{
		Clock:          fixedClock("2026-05-05T17:00:00Z"),
		CommandHandler: handler,
	})

	if err := server.HandlePayload(context.Background(), transport, readFixture(t, "audio-clear.valid.json")); err != nil {
		t.Fatalf("handle audio.clear: %v", err)
	}
	if handler.calls != 1 {
		t.Fatalf("handler calls = %d, want 1", handler.calls)
	}
	if handler.command.Type != "audio.clear" || handler.command.Scope != "queued" || handler.command.Reason != "barge_in" {
		t.Fatalf("command = %+v, want queued barge_in audio.clear", handler.command)
	}
}

func TestHelloCanAdvertiseMediaClearCapabilities(t *testing.T) {
	capabilities := MediaCapabilities()
	server := NewServer(Options{
		BridgeID:      "bridge_loopback_poc",
		BridgeVersion: "0.1.0",
		Clock:         fixedClock("2026-05-05T17:00:00Z"),
		Capabilities:  &capabilities,
	})

	hello := server.Hello()
	if !hello.Capabilities.ClearQueuedAudio || !hello.Capabilities.BargeIn {
		t.Fatalf("capabilities = %+v, want media clear/barge-in advertised", hello.Capabilities)
	}
}

func TestCommandHandlerErrorReturnsProtocolErrorWithCallID(t *testing.T) {
	transport := &fakeTransport{}
	handler := &fakeCommandHandler{
		err: NewCommandError("call_not_found", "Call was not found.\n", false, "call_in_000001"),
	}
	server := NewServer(Options{
		Clock:          fixedClock("2026-05-05T17:00:00Z"),
		CommandHandler: handler,
	})

	if err := server.HandlePayload(context.Background(), transport, readFixture(t, "call-answer.valid.json")); err != nil {
		t.Fatalf("handle call.answer: %v", err)
	}
	if len(transport.sent) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(transport.sent))
	}

	var event ErrorEvent
	if err := json.Unmarshal(transport.sent[0], &event); err != nil {
		t.Fatalf("unmarshal error event: %v", err)
	}
	if event.Error.Code != "call_not_found" {
		t.Fatalf("error code = %q, want call_not_found", event.Error.Code)
	}
	if event.Error.CommandID != "cmd_answer_000001" {
		t.Fatalf("commandId = %q, want cmd_answer_000001", event.Error.CommandID)
	}
	if event.Error.CallID != "call_in_000001" {
		t.Fatalf("callId = %q, want call_in_000001", event.Error.CallID)
	}
	if event.Error.Message != "Call was not found." {
		t.Fatalf("message = %q, want sanitized message", event.Error.Message)
	}
}

func assertMessageType(t *testing.T, payload []byte, want string) {
	t.Helper()
	var envelope struct {
		ProtocolVersion string `json:"protocolVersion"`
		Type            string `json:"type"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if envelope.ProtocolVersion != Version {
		t.Fatalf("protocolVersion = %q, want %q", envelope.ProtocolVersion, Version)
	}
	if envelope.Type != want {
		t.Fatalf("type = %q, want %q", envelope.Type, want)
	}
}

func assertJSONEqualFixture(t *testing.T, got any, fixtureName string) {
	t.Helper()

	gotPayload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}
	var gotJSON any
	if err := json.Unmarshal(gotPayload, &gotJSON); err != nil {
		t.Fatalf("unmarshal generated JSON: %v", err)
	}
	var wantJSON any
	if err := json.Unmarshal(readFixture(t, fixtureName), &wantJSON); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("message mismatch\n got: %s\nwant: %s", gotPayload, readFixture(t, fixtureName))
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(repoRoot(t), "protocol", "fixtures", "valid", name)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return payload
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", ".."))
}

func fixedClock(value string) Clock {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return func() time.Time {
		return parsed
	}
}

type fakeTransport struct {
	incoming [][]byte
	sent     [][]byte
	closed   bool
}

func (f *fakeTransport) Send(_ context.Context, payload []byte) error {
	f.sent = append(f.sent, append([]byte(nil), payload...))
	return nil
}

func (f *fakeTransport) Receive(_ context.Context) ([]byte, error) {
	if len(f.incoming) == 0 {
		return nil, io.EOF
	}
	payload := f.incoming[0]
	f.incoming = f.incoming[1:]
	return payload, nil
}

func (f *fakeTransport) Close() error {
	f.closed = true
	return nil
}

type fakeCommandHandler struct {
	command Command
	err     error
	calls   int
}

func (h *fakeCommandHandler) HandleCommand(_ context.Context, command Command) error {
	h.calls++
	h.command = command
	return h.err
}
