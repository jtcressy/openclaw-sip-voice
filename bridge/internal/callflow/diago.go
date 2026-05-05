package callflow

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"github.com/emiago/diago"
	diagomedia "github.com/emiago/diago/media"
	"github.com/emiago/sipgo/sip"

	bridgemedia "github.com/jtcressy/openclaw-sip-voice/bridge/internal/media"
)

const defaultInviteTimeout = 30 * time.Second

type DiagoDialerOptions struct {
	Server        string
	Username      string
	Password      string
	Extension     string
	Transport     string
	InviteTimeout time.Duration
}

type DiagoDialer struct {
	Diago *diago.Diago
	DiagoDialerOptions
}

func (d DiagoDialer) Dial(ctx context.Context, req OutboundRequest) (OutboundLeg, error) {
	if d.Diago == nil {
		return nil, errors.New("SIP outbound dialer is not configured")
	}
	host, port := splitServerHostPort(d.Server)
	if host == "" {
		return nil, errors.New("SIP outbound server is not configured")
	}

	timeout := d.InviteTimeout
	if timeout == 0 {
		timeout = defaultInviteTimeout
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialog, err := d.Diago.Invite(dialCtx, sip.Uri{
		Scheme:    "sip",
		User:      req.Remote.Handle,
		Host:      host,
		Port:      port,
		UriParams: sip.NewParams(),
	}, diago.InviteOptions{
		Transport: d.Transport,
		Username:  d.Username,
		Password:  d.Password,
		Headers:   d.identityHeaders(host),
	})
	if err != nil {
		return nil, err
	}
	return diagoOutboundLeg{dialog: dialog}, nil
}

func (d DiagoDialer) identityHeaders(host string) []sip.Header {
	if d.Username == "" {
		return []sip.Header{}
	}
	displayName := d.Extension
	if displayName == "" {
		displayName = d.Username
	}
	return []sip.Header{
		&sip.FromHeader{
			DisplayName: displayName,
			Address: sip.Uri{
				Scheme: "sip",
				User:   d.Username,
				Host:   host,
			},
			Params: sip.NewParams(),
		},
	}
}

func (m *Manager) HandleDiagoInbound(dialog *diago.DialogServerSession) {
	if dialog == nil {
		return
	}
	callID, err := m.StartInbound(dialog.Context(), InboundInvite{
		Context: dialog.Context(),
		Caller:  dialog.FromUser(),
		Callee:  dialog.ToUser(),
		Leg:     diagoInboundLeg{dialog: dialog},
	})
	if err != nil {
		return
	}
	m.Wait(callID)
}

type diagoInboundLeg struct {
	dialog *diago.DialogServerSession
}

func (l diagoInboundLeg) Ringing() error {
	return l.dialog.Ringing()
}

func (l diagoInboundLeg) Answer() error {
	return l.dialog.Answer()
}

func (l diagoInboundLeg) Hangup(ctx context.Context) error {
	return l.dialog.Hangup(ctx)
}

func (l diagoInboundLeg) MediaEndpoints(ctx context.Context) (bridgemedia.Endpoints, error) {
	return diagoMediaEndpoints(ctx, l.dialog.Media())
}

type diagoOutboundLeg struct {
	dialog *diago.DialogClientSession
}

func (l diagoOutboundLeg) Hangup(ctx context.Context) error {
	return l.dialog.Hangup(ctx)
}

func (l diagoOutboundLeg) MediaEndpoints(ctx context.Context) (bridgemedia.Endpoints, error) {
	return diagoMediaEndpoints(ctx, l.dialog.Media())
}

func diagoMediaEndpoints(_ context.Context, dialogMedia *diago.DialogMedia) (bridgemedia.Endpoints, error) {
	if dialogMedia == nil {
		return bridgemedia.Endpoints{}, errors.New("media is not available")
	}
	readerProps := diago.MediaProps{}
	reader, err := dialogMedia.AudioReader(diago.WithAudioReaderMediaProps(&readerProps))
	if err != nil {
		return bridgemedia.Endpoints{}, errors.New("media reader is not available")
	}
	writerProps := diago.MediaProps{}
	writer, err := dialogMedia.AudioWriter(diago.WithAudioWriterMediaProps(&writerProps))
	if err != nil {
		return bridgemedia.Endpoints{}, errors.New("media writer is not available")
	}

	codec := bridgeCodec(readerProps.Codec)
	if codec == bridgemedia.CodecUnsupported {
		codec = bridgeCodec(writerProps.Codec)
	}
	return bridgemedia.Endpoints{
		Reader:      reader,
		Writer:      writer,
		Codec:       codec,
		WriterPaces: true,
	}, nil
}

func bridgeCodec(codec diagomedia.Codec) bridgemedia.Codec {
	switch codec.Name {
	case diagomedia.CodecAudioUlaw.Name:
		return bridgemedia.CodecPCMU
	case diagomedia.CodecAudioAlaw.Name:
		return bridgemedia.CodecPCMA
	default:
		return bridgemedia.CodecUnsupported
	}
}

func splitServerHostPort(value string) (string, int) {
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return value, 0
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return value, 0
	}
	return host, port
}
