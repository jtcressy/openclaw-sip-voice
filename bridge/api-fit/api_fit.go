// Package apifit is a disposable compile/API-fit spike for a Go SIP UA/media bridge.
package apifit

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/diago/media/sdp"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
)

const (
	SpikeUserAgent = "openclaw-bridge-api-fit"
	SpikeTransport = "udp"
	SpikeSIPPort   = 5060
)

// Compile-time checks for SIP UA, client, server, digest registration,
// dialog handling, and encoded RTP media APIs.
var (
	_ func(options ...sipgo.UserAgentOption) (*sipgo.UserAgent, error)     = sipgo.NewUA
	_ func(*sipgo.UserAgent, ...sipgo.ClientOption) (*sipgo.Client, error) = sipgo.NewClient
	_ func(*sipgo.UserAgent, ...sipgo.ServerOption) (*sipgo.Server, error) = sipgo.NewServer

	_ func(*sipgo.Client, context.Context, *sip.Request, ...sipgo.ClientRequestOption) (*sip.Response, error)    = (*sipgo.Client).Do
	_ func(*sipgo.Client, context.Context, *sip.Request, *sip.Response, sipgo.DigestAuth) (*sip.Response, error) = (*sipgo.Client).DoDigestAuth
	_ sipgo.ClientRequestOption                                                                                  = sipgo.ClientRequestRegisterBuild

	_ func(*diago.Diago, context.Context, sip.Uri, diago.RegisterOptions) (*diago.RegisterTransaction, error) = (*diago.Diago).RegisterTransaction
	_ func(*diago.RegisterTransaction, context.Context) error                                                 = (*diago.RegisterTransaction).Register
	_ func(*diago.RegisterTransaction, context.Context) error                                                 = (*diago.RegisterTransaction).Qualify
	_ func(*diago.RegisterTransaction, context.Context) error                                                 = (*diago.RegisterTransaction).QualifyLoop
	_ func(*diago.RegisterTransaction, context.Context) error                                                 = (*diago.RegisterTransaction).Unregister

	_ func(*sipgo.Server, sipgo.RequestHandler) = (*sipgo.Server).OnInvite
	_ func(*sipgo.Server, sipgo.RequestHandler) = (*sipgo.Server).OnAck
	_ func(*sipgo.Server, sipgo.RequestHandler) = (*sipgo.Server).OnBye
	_ func(*sipgo.Server, sipgo.RequestHandler) = (*sipgo.Server).OnCancel

	_ func(*diago.Diago, context.Context, diago.ServeDialogFunc) error            = (*diago.Diago).Serve
	_ func(*diago.Diago, context.Context, diago.ServeDialogFunc) error            = (*diago.Diago).ServeBackground
	_ func(*diago.DialogServerSession) error                                      = (*diago.DialogServerSession).Trying
	_ func(*diago.DialogServerSession) error                                      = (*diago.DialogServerSession).Ringing
	_ func(*diago.DialogServerSession) error                                      = (*diago.DialogServerSession).Answer
	_ func(*diago.DialogServerSession, *sip.Request, sip.ServerTransaction) error = (*diago.DialogServerSession).ReadAck
	_ func(*diago.DialogServerSession, *sip.Request, sip.ServerTransaction) error = (*diago.DialogServerSession).ReadBye
	_ func(*diago.DialogServerSession, context.Context) error                     = (*diago.DialogServerSession).Hangup

	_ func(*diago.Diago, sip.Uri, diago.NewDialogOptions) (*diago.DialogClientSession, error)               = (*diago.Diago).NewDialog
	_ func(*diago.Diago, context.Context, sip.Uri, diago.InviteOptions) (*diago.DialogClientSession, error) = (*diago.Diago).Invite
	_ func(*diago.DialogClientSession, context.Context, diago.InviteClientOptions) error                    = (*diago.DialogClientSession).Invite
	_ func(*diago.DialogClientSession, context.Context) error                                               = (*diago.DialogClientSession).Ack
	_ func(*diago.DialogClientSession, context.Context) error                                               = (*diago.DialogClientSession).Hangup

	_ func(*diago.DialogMedia, ...diago.AudioReaderOption) (io.Reader, error) = (*diago.DialogMedia).AudioReader
	_ func(*diago.DialogMedia, ...diago.AudioWriterOption) (io.Writer, error) = (*diago.DialogMedia).AudioWriter
	_ func(media.RTPReader, media.Codec) *media.RTPPacketReader               = media.NewRTPPacketReader
	_ func(media.RTPWriter, media.Codec) *media.RTPPacketWriter               = media.NewRTPPacketWriter
	_ func(*media.RTPPacketWriter, []byte, uint32, bool, uint8) (int, error)  = (*media.RTPPacketWriter).WriteSamples
)

// NewSpikeUA constructs the low-level SIP user agent/client/server primitives.
func NewSpikeUA(macvlanIP string) (*sipgo.UserAgent, *sipgo.Client, *sipgo.Server, error) {
	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent(SpikeUserAgent),
		sipgo.WithUserAgentHostname(macvlanIP),
	)
	if err != nil {
		return nil, nil, nil, err
	}

	client, err := sipgo.NewClient(
		ua,
		sipgo.WithClientNAT(),
		sipgo.WithClientHostname(macvlanIP),
		sipgo.WithClientPort(SpikeSIPPort),
		sipgo.WithClientConnectionAddr(net.JoinHostPort(macvlanIP, "0")),
	)
	if err != nil {
		_ = ua.Close()
		return nil, nil, nil, err
	}

	server, err := sipgo.NewServer(ua)
	if err != nil {
		_ = ua.Close()
		return nil, nil, nil, err
	}

	return ua, client, server, nil
}

// RegisterInboundHandlers wires the SIP methods needed for an inbound call leg.
func RegisterInboundHandlers(server *sipgo.Server, handler sipgo.RequestHandler) {
	server.OnInvite(handler)
	server.OnAck(handler)
	server.OnBye(handler)
	server.OnCancel(handler)
}

// NewSpikeDiago wires diago with explicit Contact and SDP advertised IP control.
func NewSpikeDiago(ua *sipgo.UserAgent, macvlanIP string) *diago.Diago {
	ip := net.ParseIP(macvlanIP)
	return diago.NewDiago(
		ua,
		diago.WithTransport(diago.Transport{
			ID:              "macvlan-udp",
			Transport:       SpikeTransport,
			BindHost:        macvlanIP,
			BindPort:        SpikeSIPPort,
			ExternalHost:    macvlanIP,
			ExternalPort:    SpikeSIPPort,
			MediaExternalIP: ip,
			RewriteContact:  false,
		}),
		diago.WithMediaConfig(diago.MediaConfig{
			Codecs: []media.Codec{media.CodecAudioUlaw, media.CodecTelephoneEvent8000},
		}),
	)
}

// NewRegisterTransaction prepares REGISTER state with digest credentials carried
// by diago.RegisterOptions. The transaction exposes Register, Qualify/refresh,
// QualifyLoop, and Unregister methods.
func NewRegisterTransaction(ctx context.Context, dg *diago.Diago, registrarHost, username, password string, expiry time.Duration) (*diago.RegisterTransaction, error) {
	return dg.RegisterTransaction(ctx, sip.Uri{
		Scheme:    "sip",
		Host:      registrarHost,
		UriParams: sip.NewParams(),
	}, diago.RegisterOptions{
		Username: username,
		Password: password,
		Expiry:   expiry,
		AllowHeaders: []string{
			sip.INVITE.String(),
			sip.ACK.String(),
			sip.BYE.String(),
			sip.CANCEL.String(),
			sip.OPTIONS.String(),
		},
	})
}

// BuildOutboundCall creates the outbound dialog object; DialogClientSession.Invite
// followed by Ack places and confirms the call when run against a SIP peer.
func BuildOutboundCall(dg *diago.Diago, destinationUser, destinationHost string) (*diago.DialogClientSession, error) {
	return dg.NewDialog(sip.Uri{
		Scheme: "sip",
		User:   destinationUser,
		Host:   destinationHost,
	}, diago.NewDialogOptions{Transport: SpikeTransport})
}

// BuildLocalPCMUOffer proves the advertised SDP connection address can be set
// independently of the local bind address.
func BuildLocalPCMUOffer(bindIP, advertisedIP string, rtpPort int) []byte {
	session := &media.MediaSession{
		Codecs:     []media.Codec{media.CodecAudioUlaw},
		Mode:       sdp.ModeSendrecv,
		Laddr:      net.UDPAddr{IP: net.ParseIP(bindIP), Port: rtpPort},
		ExternalIP: net.ParseIP(advertisedIP),
	}
	return session.LocalSDP()
}

// NewPCMUEncodedSurfaces returns the encoded RTP payload reader/writer surfaces
// used for 20 ms g711_ulaw bridging. No PCM transcoding API is required here.
func NewPCMUEncodedSurfaces(reader media.RTPReader, writer media.RTPWriter) (io.Reader, io.Writer, *media.RTPPacketReader, *media.RTPPacketWriter) {
	rtpReader := media.NewRTPPacketReader(reader, media.CodecAudioUlaw)
	rtpWriter := media.NewRTPPacketWriter(writer, media.CodecAudioUlaw)
	return rtpReader, rtpWriter, rtpReader, rtpWriter
}

// WritePCMU20ms writes one 20 ms PCMU frame using explicit RTP timing.
func WritePCMU20ms(writer *media.RTPPacketWriter, payload []byte, marker bool) (int, error) {
	return writer.WriteSamples(payload, media.CodecAudioUlaw.SampleTimestamp(), marker, media.CodecAudioUlaw.PayloadType)
}

type RTPPacket = rtp.Packet
