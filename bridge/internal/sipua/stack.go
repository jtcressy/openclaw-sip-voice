package sipua

import (
	"fmt"
	"net"

	"github.com/emiago/diago"
	diagomedia "github.com/emiago/diago/media"
	"github.com/emiago/sipgo"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
)

const UserAgent = "openclaw-sip-voice-bridge"

type Stack struct {
	UA     *sipgo.UserAgent
	Client *sipgo.Client
	Server *sipgo.Server
	Diago  *diago.Diago
}

func NewStack(cfg config.Config) (*Stack, error) {
	diagomedia.RTPPortStart = cfg.RTPPortMin
	diagomedia.RTPPortEnd = cfg.RTPPortMax

	ua, err := sipgo.NewUA(
		sipgo.WithUserAgent(userAgentName(cfg)),
		sipgo.WithUserAgentHostname(cfg.SIPAdvertiseAddr.Host),
	)
	if err != nil {
		return nil, err
	}

	client, err := sipgo.NewClient(
		ua,
		sipgo.WithClientNAT(),
		sipgo.WithClientHostname(cfg.SIPAdvertiseAddr.Host),
		sipgo.WithClientPort(cfg.SIPAdvertiseAddr.Port),
		sipgo.WithClientConnectionAddr(net.JoinHostPort(cfg.SIPBindAddr.Host, "0")),
	)
	if err != nil {
		_ = ua.Close()
		return nil, err
	}

	server, err := sipgo.NewServer(ua)
	if err != nil {
		_ = ua.Close()
		return nil, err
	}

	diagoStack := diago.NewDiago(
		ua,
		diago.WithClient(client),
		diago.WithServer(server),
		diago.WithTransport(diago.Transport{
			ID:              "unifi-talk-" + cfg.SIPTransport,
			Transport:       cfg.SIPTransport,
			BindHost:        cfg.SIPBindAddr.Host,
			BindPort:        cfg.SIPBindAddr.Port,
			ExternalHost:    cfg.SIPAdvertiseAddr.Host,
			ExternalPort:    cfg.SIPAdvertiseAddr.Port,
			MediaExternalIP: net.ParseIP(cfg.SIPAdvertiseAddr.Host),
			RewriteContact:  false,
		}),
		diago.WithMediaConfig(diago.MediaConfig{
			Codecs: mediaCodecs(cfg.Codecs),
		}),
	)

	return &Stack{
		UA:     ua,
		Client: client,
		Server: server,
		Diago:  diagoStack,
	}, nil
}

func userAgentName(cfg config.Config) string {
	if cfg.UniFiConfigured() {
		return cfg.UniFiTalkSIPUsername
	}
	return UserAgent
}

func (s *Stack) Close() error {
	if s == nil {
		return nil
	}
	if s.Client != nil {
		_ = s.Client.Close()
	}
	if s.Server != nil {
		_ = s.Server.Close()
	}
	if s.UA != nil {
		return s.UA.Close()
	}
	return nil
}

func mediaCodecs(names []string) []diagomedia.Codec {
	codecs := make([]diagomedia.Codec, 0, len(names))
	for _, name := range names {
		switch name {
		case "g711_ulaw":
			codecs = append(codecs, diagomedia.CodecAudioUlaw)
		default:
			panic(fmt.Sprintf("unsupported codec %q passed validated config boundary", name))
		}
	}
	return codecs
}
