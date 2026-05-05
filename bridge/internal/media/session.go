package media

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
)

type SessionOptions struct {
	CallID    string
	Endpoints Endpoints
	Clock     Clock
	EventSink EventSink
	Pacer     Pacer
	QueueSize int
}

type Session struct {
	callID    string
	endpoints Endpoints
	clock     Clock
	sink      EventSink
	pacer     Pacer
	queue     chan Frame

	cancel    context.CancelFunc
	closeOnce sync.Once
	closed    chan struct{}
}

func NewSession(opts SessionOptions) *Session {
	clock := opts.Clock
	if clock == nil {
		clock = time.Now
	}
	pacer := opts.Pacer
	if pacer == nil {
		pacer = RealPacer{Duration: FrameDuration}
	}
	if opts.Endpoints.WriterPaces {
		pacer = NoopPacer{}
	}
	queueSize := opts.QueueSize
	if queueSize <= 0 {
		queueSize = DefaultQueueSize
	}
	return &Session{
		callID:    opts.CallID,
		endpoints: opts.Endpoints,
		clock:     clock,
		sink:      opts.EventSink,
		pacer:     pacer,
		queue:     make(chan Frame, queueSize),
		closed:    make(chan struct{}),
	}
}

func (s *Session) Start(ctx context.Context) error {
	if s.callID == "" {
		return errors.New("media call id is not configured")
	}
	if s.endpoints.Codec != CodecPCMU {
		return ErrUnsupportedCodec
	}
	if s.endpoints.Reader == nil || s.endpoints.Writer == nil {
		return errors.New("media endpoints are not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	go s.readLoop(sessionCtx)
	go s.writeLoop(sessionCtx)
	return nil
}

func (s *Session) Enqueue(ctx context.Context, frame Frame) error {
	if err := ValidatePCMUFramePayload(frame.Payload); err != nil {
		return err
	}
	payload := append([]byte(nil), frame.Payload...)
	queued := Frame{
		Sequence: frame.Sequence,
		Payload:  payload,
	}
	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case <-s.closed:
		return ErrSessionClosed
	default:
	}

	select {
	case s.queue <- queued:
		return nil
	case <-s.closed:
		return ErrSessionClosed
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrQueueFull
	}
}

func (s *Session) ClearOutbound() int {
	dropped := 0
	for {
		select {
		case <-s.queue:
			dropped++
		default:
			return dropped
		}
	}
}

func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		close(s.closed)
	})
	return nil
}

func (s *Session) Codec() Codec {
	return s.endpoints.Codec
}

func (s *Session) readLoop(ctx context.Context) {
	buf := make([]byte, FrameBytes)
	sequence := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if _, err := io.ReadFull(s.endpoints.Reader, buf); err != nil {
			return
		}
		if s.sink != nil {
			payload := append([]byte(nil), buf...)
			event := protocol.NewAudioInEvent(s.clock(), s.callID, sequence, sequence*int(FrameDuration/time.Millisecond), payload)
			_ = s.sink.Publish(ctx, event)
		}
		sequence++
	}
}

func (s *Session) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-s.queue:
			if _, err := s.endpoints.Writer.Write(frame.Payload); err != nil {
				return
			}
			if err := s.pacer.Wait(ctx); err != nil {
				return
			}
		}
	}
}
