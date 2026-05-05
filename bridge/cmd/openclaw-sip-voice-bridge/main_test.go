package main

import (
	"testing"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
)

func TestBridgeCapabilitiesReflectUniFiConfiguration(t *testing.T) {
	unconfigured, err := config.ParseEnv(nil)
	if err != nil {
		t.Fatalf("parse unconfigured env: %v", err)
	}
	if got := bridgeCapabilities(unconfigured); got.InboundCalls || got.OutboundCalls || got.BargeIn || got.ClearQueuedAudio {
		t.Fatalf("unconfigured capabilities = %+v, want all false", got)
	}

	configured, err := config.ParseEnv([]string{
		"UNIFI_TALK_SIP_SERVER=192.168.20.1",
		"UNIFI_TALK_SIP_USERNAME=openclaw",
		"UNIFI_TALK_SIP_PASSWORD=secret",
		"UNIFI_TALK_SIP_EXTENSION=1234",
	})
	if err != nil {
		t.Fatalf("parse configured env: %v", err)
	}
	if got := bridgeCapabilities(configured); !got.InboundCalls || !got.OutboundCalls || !got.BargeIn || !got.ClearQueuedAudio {
		t.Fatalf("configured capabilities = %+v, want call/media capabilities", got)
	}
}
