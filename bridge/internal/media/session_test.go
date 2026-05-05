package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
)

func TestSessionPublishesCanonicalAudioInFrames(t *testing.T) {
	first := bytes.Repeat([]byte{0x7f}, FrameBytes)
	second := bytes.Repeat([]byte{0xff}, FrameBytes)
	reader := bytes.NewReader(append(append([]byte{}, first...), second...))
	sink := &recordingSink{}
	session := NewSession(SessionOptions{
		CallID: "call_in_000001",
		Endpoints: Endpoints{
			Reader: reader,
			Writer: &recordingWriter{},
			Codec:  CodecPCMU,
		},
		Clock:     fixedClock(),
		EventSink: sink,
		Pacer:     NoopPacer{},
	})

	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	waitFor(t, func() bool {
		return sink.audioInCount() == 2
	})

	events := sink.audioInEvents()
	if events[0].CallID != "call_in_000001" || events[0].Sequence != 0 || events[0].TimestampMs != 0 {
		t.Fatalf("first audio.in = %+v, want sequence/timestamp 0", events[0])
	}
	if events[1].Sequence != 1 || events[1].TimestampMs != 20 {
		t.Fatalf("second audio.in = %+v, want sequence 1 timestamp 20", events[1])
	}
	if events[0].Audio.AudioFormat() != protocol.CanonicalAudioFormat() {
		t.Fatalf("audio format = %+v, want canonical", events[0].Audio)
	}
	payload, err := base64.StdEncoding.DecodeString(events[0].Audio.Payload)
	if err != nil {
		t.Fatalf("decode audio payload: %v", err)
	}
	if !bytes.Equal(payload, first) {
		t.Fatal("audio.in payload did not preserve PCMU frame bytes")
	}
}

func TestSessionWritesOutboundFramesWithPacing(t *testing.T) {
	writer := &recordingWriter{}
	pacer := newManualPacer()
	session := NewSession(SessionOptions{
		CallID: "call_in_000001",
		Endpoints: Endpoints{
			Reader: bytes.NewReader(nil),
			Writer: writer,
			Codec:  CodecPCMU,
		},
		Pacer: pacer,
	})

	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	first := bytes.Repeat([]byte{0x01}, FrameBytes)
	second := bytes.Repeat([]byte{0x02}, FrameBytes)
	if err := session.Enqueue(context.Background(), Frame{Sequence: 0, Payload: first}); err != nil {
		t.Fatalf("enqueue first frame: %v", err)
	}
	if err := session.Enqueue(context.Background(), Frame{Sequence: 1, Payload: second}); err != nil {
		t.Fatalf("enqueue second frame: %v", err)
	}

	waitFor(t, func() bool {
		return writer.count() == 1
	})
	if writer.count() != 1 {
		t.Fatalf("writes before pace release = %d, want 1", writer.count())
	}

	pacer.Step()
	waitFor(t, func() bool {
		return writer.count() == 2
	})

	writes := writer.payloads()
	if !bytes.Equal(writes[0], first) || !bytes.Equal(writes[1], second) {
		t.Fatal("writer did not receive outbound frames in order")
	}
}

func TestSessionClearOutboundDropsQueuedFrames(t *testing.T) {
	writer := &recordingWriter{}
	pacer := newManualPacer()
	session := NewSession(SessionOptions{
		CallID: "call_in_000001",
		Endpoints: Endpoints{
			Reader: bytes.NewReader(nil),
			Writer: writer,
			Codec:  CodecPCMU,
		},
		Pacer: pacer,
	})

	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := session.Enqueue(context.Background(), Frame{Sequence: i, Payload: bytes.Repeat([]byte{byte(i)}, FrameBytes)}); err != nil {
			t.Fatalf("enqueue frame %d: %v", i, err)
		}
	}
	waitFor(t, func() bool {
		return writer.count() == 1
	})

	if dropped := session.ClearOutbound(); dropped != 2 {
		t.Fatalf("dropped frames = %d, want 2", dropped)
	}
	pacer.Step()
	time.Sleep(10 * time.Millisecond)
	if writer.count() != 1 {
		t.Fatalf("writes after clear = %d, want only in-flight frame", writer.count())
	}
}

func TestSessionRejectsUnsupportedCodec(t *testing.T) {
	session := NewSession(SessionOptions{
		CallID: "call_in_000001",
		Endpoints: Endpoints{
			Reader: bytes.NewReader(bytes.Repeat([]byte{0xff}, FrameBytes)),
			Writer: &recordingWriter{},
			Codec:  CodecPCMA,
		},
		EventSink: &recordingSink{},
	})

	err := session.Start(context.Background())
	if !errors.Is(err, ErrUnsupportedCodec) {
		t.Fatalf("start error = %v, want ErrUnsupportedCodec", err)
	}
}

func TestSessionRejectsNonCanonicalOutboundFrame(t *testing.T) {
	session := NewSession(SessionOptions{
		CallID: "call_in_000001",
		Endpoints: Endpoints{
			Reader: bytes.NewReader(nil),
			Writer: &recordingWriter{},
			Codec:  CodecPCMU,
		},
		Pacer: NoopPacer{},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("start session: %v", err)
	}

	err := session.Enqueue(context.Background(), Frame{Payload: bytes.Repeat([]byte{0xff}, FrameBytes+1)})
	if !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("enqueue error = %v, want ErrInvalidFrame", err)
	}
}

func fixedClock() Clock {
	return func() time.Time {
		return time.Date(2026, 5, 5, 17, 0, 5, 0, time.UTC)
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

func (s *recordingSink) audioInCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, event := range s.events {
		if _, ok := event.(protocol.AudioInEvent); ok {
			count++
		}
	}
	return count
}

func (s *recordingSink) audioInEvents() []protocol.AudioInEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := []protocol.AudioInEvent{}
	for _, event := range s.events {
		if audioIn, ok := event.(protocol.AudioInEvent); ok {
			events = append(events, audioIn)
		}
	}
	return events
}

type recordingWriter struct {
	mu            sync.Mutex
	payloadsValue [][]byte
}

func (w *recordingWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.payloadsValue = append(w.payloadsValue, append([]byte(nil), payload...))
	return len(payload), nil
}

func (w *recordingWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.payloadsValue)
}

func (w *recordingWriter) payloads() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	payloads := make([][]byte, len(w.payloadsValue))
	for i := range w.payloadsValue {
		payloads[i] = append([]byte(nil), w.payloadsValue[i]...)
	}
	return payloads
}

type manualPacer struct {
	wait chan struct{}
}

func newManualPacer() *manualPacer {
	return &manualPacer{wait: make(chan struct{}, 8)}
}

func (p *manualPacer) Wait(ctx context.Context) error {
	select {
	case <-p.wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *manualPacer) Step() {
	p.wait <- struct{}{}
}
