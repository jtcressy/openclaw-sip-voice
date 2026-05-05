package media

import "context"

type SessionHandle interface {
	Enqueue(context.Context, Frame) error
	ClearOutbound() int
	Close() error
}

type Factory struct {
	EventSink EventSink
	Clock     Clock
	Pacer     Pacer
	QueueSize int
}

func (f Factory) Attach(ctx context.Context, callID string, endpoints Endpoints) (SessionHandle, error) {
	session := NewSession(SessionOptions{
		CallID:    callID,
		Endpoints: endpoints,
		Clock:     f.Clock,
		EventSink: f.EventSink,
		Pacer:     f.Pacer,
		QueueSize: f.QueueSize,
	})
	if err := session.Start(ctx); err != nil {
		return nil, err
	}
	return session, nil
}
