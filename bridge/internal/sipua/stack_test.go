package sipua

import (
	"testing"

	diagomedia "github.com/emiago/diago/media"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
)

func TestNewStackConstructsSipgoDiagoSurfaces(t *testing.T) {
	restoreRTPRange := saveRTPRange()
	t.Cleanup(restoreRTPRange)

	cfg, err := config.ParseEnv([]string{
		"SIP_BIND_ADDR=127.0.0.1:5060",
		"SIP_ADVERTISE_ADDR=127.0.0.1:5060",
		"RTP_PORT_MIN=12000",
		"RTP_PORT_MAX=12019",
	})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	stack, err := NewStack(cfg)
	if err != nil {
		t.Fatalf("new stack: %v", err)
	}
	defer stack.Close()

	if stack.UA == nil {
		t.Fatal("UA is nil")
	}
	if stack.UA.Name() != UserAgent {
		t.Fatalf("UA name = %q, want default user agent", stack.UA.Name())
	}
	if stack.Client == nil {
		t.Fatal("Client is nil")
	}
	if stack.Server == nil {
		t.Fatal("Server is nil")
	}
	if stack.Diago == nil {
		t.Fatal("Diago is nil")
	}
	if diagomedia.RTPPortStart != 12000 || diagomedia.RTPPortEnd != 12019 {
		t.Fatalf("RTP range = %d-%d, want 12000-12019", diagomedia.RTPPortStart, diagomedia.RTPPortEnd)
	}
}

func TestNewStackUsesUniFiUsernameForSIPContactIdentity(t *testing.T) {
	restoreRTPRange := saveRTPRange()
	t.Cleanup(restoreRTPRange)

	cfg, err := config.ParseEnv([]string{
		"UNIFI_TALK_SIP_SERVER=192.168.20.1",
		"UNIFI_TALK_SIP_USERNAME=openclaw-line",
		"UNIFI_TALK_SIP_PASSWORD=secret",
		"UNIFI_TALK_SIP_EXTENSION=4321",
		"SIP_BIND_ADDR=127.0.0.1:5060",
		"SIP_ADVERTISE_ADDR=127.0.0.1:5060",
	})
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	stack, err := NewStack(cfg)
	if err != nil {
		t.Fatalf("new stack: %v", err)
	}
	defer stack.Close()

	if stack.UA.Name() != "openclaw-line" {
		t.Fatalf("UA name = %q, want UniFi SIP username", stack.UA.Name())
	}
}

func saveRTPRange() func() {
	start := diagomedia.RTPPortStart
	end := diagomedia.RTPPortEnd
	return func() {
		diagomedia.RTPPortStart = start
		diagomedia.RTPPortEnd = end
	}
}
