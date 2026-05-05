package media

import (
	"context"
	"errors"
	"io"
	"time"
)

const (
	FrameBytes       = 160
	FrameDuration    = 20 * time.Millisecond
	DefaultQueueSize = 50
)

type Codec string

const (
	CodecPCMU        Codec = "pcmu"
	CodecPCMA        Codec = "pcma"
	CodecUnsupported Codec = "unsupported"
)

var (
	ErrUnsupportedCodec = errors.New("unsupported media codec")
	ErrInvalidFrame     = errors.New("invalid media frame")
	ErrQueueFull        = errors.New("media outbound queue full")
	ErrSessionClosed    = errors.New("media session closed")
)

type Endpoints struct {
	Reader      io.Reader
	Writer      io.Writer
	Codec       Codec
	WriterPaces bool
}

type Frame struct {
	Sequence int
	Payload  []byte
}

type Clock func() time.Time

type EventSink interface {
	Publish(context.Context, any) error
}

type Pacer interface {
	Wait(context.Context) error
}

type PacerFunc func(context.Context) error

func (f PacerFunc) Wait(ctx context.Context) error {
	return f(ctx)
}

type RealPacer struct {
	Duration time.Duration
}

func (p RealPacer) Wait(ctx context.Context) error {
	duration := p.Duration
	if duration == 0 {
		duration = FrameDuration
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type NoopPacer struct{}

func (NoopPacer) Wait(context.Context) error {
	return nil
}

func ValidatePCMUFramePayload(payload []byte) error {
	if len(payload) != FrameBytes {
		return ErrInvalidFrame
	}
	return nil
}
