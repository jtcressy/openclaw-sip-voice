package config

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseEnvDefaultsAndRedaction(t *testing.T) {
	cfg, err := ParseEnv(nil)
	if err != nil {
		t.Fatalf("parse defaults: %v", err)
	}

	if cfg.UniFiConfigured() {
		t.Fatal("empty environment should not configure UniFi registration")
	}
	if cfg.SIPTransport != "udp" {
		t.Fatalf("SIPTransport = %q, want udp", cfg.SIPTransport)
	}
	if got := cfg.BridgeWSAddr.String(); got != DefaultBridgeWSAddr {
		t.Fatalf("BridgeWSAddr = %q, want %q", got, DefaultBridgeWSAddr)
	}
	if got := strings.Join(cfg.Codecs, ","); got != DefaultCodecs {
		t.Fatalf("Codecs = %q, want %q", got, DefaultCodecs)
	}

	redacted := cfg.RedactedMap()
	for _, name := range []string{
		EnvUniFiTalkSIPServer,
		EnvUniFiTalkSIPUsername,
		EnvUniFiTalkSIPPassword,
		EnvUniFiTalkSIPExtension,
	} {
		if redacted[name] != "(unset)" {
			t.Fatalf("%s redaction = %q, want (unset)", name, redacted[name])
		}
	}
}

func TestParseEnvRequiresCredentialGroupAndRedactsSecrets(t *testing.T) {
	cfg, err := ParseEnv([]string{
		"UNIFI_TALK_SIP_SERVER=pbx.example.test",
		"UNIFI_TALK_SIP_USERNAME=alice",
	})
	if err == nil {
		t.Fatal("partial UniFi credentials unexpectedly passed")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T, want ValidationError", err)
	}

	cfg, err = ParseEnv([]string{
		"UNIFI_TALK_SIP_SERVER=198.51.100.10",
		"UNIFI_TALK_SIP_USERNAME=alice",
		"UNIFI_TALK_SIP_PASSWORD=super-secret",
		"UNIFI_TALK_SIP_EXTENSION=1234",
	})
	if err != nil {
		t.Fatalf("complete UniFi credentials: %v", err)
	}
	if !cfg.UniFiConfigured() {
		t.Fatal("complete UniFi credentials were not marked configured")
	}

	payload, err := json.Marshal(cfg.RedactedValues())
	if err != nil {
		t.Fatalf("marshal redacted values: %v", err)
	}
	for _, leaked := range []string{"198.51.100.10", "alice", "super-secret", "1234"} {
		if strings.Contains(string(payload), leaked) {
			t.Fatalf("redacted values leaked %q in %s", leaked, payload)
		}
	}
	for _, name := range []string{
		EnvUniFiTalkSIPServer,
		EnvUniFiTalkSIPUsername,
		EnvUniFiTalkSIPPassword,
		EnvUniFiTalkSIPExtension,
	} {
		if got := cfg.RedactedMap()[name]; got != "(set)" {
			t.Fatalf("%s redaction = %q, want (set)", name, got)
		}
	}
}

func TestParseEnvValidation(t *testing.T) {
	_, err := ParseEnv([]string{
		"SIP_TRANSPORT=tcp",
		"SIP_BIND_ADDR=not-an-ip:5060",
		"SIP_ADVERTISE_ADDR=127.0.0.1:5060",
		"RTP_PORT_MIN=50000",
		"RTP_PORT_MAX=40000",
		"CODECS=g711_alaw",
	})
	if err == nil {
		t.Fatal("invalid environment unexpectedly passed")
	}

	msg := err.Error()
	for _, want := range []string{
		"SIP_TRANSPORT must be udp",
		"SIP_BIND_ADDR host must be an IP address",
		"RTP_PORT_MIN must be < RTP_PORT_MAX",
		"CODECS contains unsupported codec",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("validation error %q did not contain %q", msg, want)
		}
	}
}

func TestParseEnvAcceptsOnlyPCMUCodecForPOC(t *testing.T) {
	cfg, err := ParseEnv([]string{"CODECS=g711_ulaw"})
	if err != nil {
		t.Fatalf("PCMU-only codec config failed: %v", err)
	}
	if got := strings.Join(cfg.Codecs, ","); got != "g711_ulaw" {
		t.Fatalf("Codecs = %q, want g711_ulaw", got)
	}

	for _, value := range []string{"g711_ulaw,g711_alaw", "telephone_event_8000"} {
		_, err := ParseEnv([]string{"CODECS=" + value})
		if err == nil {
			t.Fatalf("CODECS=%s unexpectedly passed", value)
		}
	}
}

func TestParseEnvRejectsInvalidRTPRangeShape(t *testing.T) {
	_, err := ParseEnv([]string{
		"RTP_PORT_MIN=10001",
		"RTP_PORT_MAX=10019",
	})
	if err == nil {
		t.Fatal("odd RTP_PORT_MIN unexpectedly passed")
	}
	if !strings.Contains(err.Error(), "RTP_PORT_MIN must be even") {
		t.Fatalf("validation error = %q, want even RTP min problem", err)
	}

	_, err = ParseEnv([]string{
		"RTP_PORT_MIN=10000",
		"RTP_PORT_MAX=10000",
	})
	if err == nil {
		t.Fatal("single-port RTP range unexpectedly passed")
	}
	if !strings.Contains(err.Error(), "RTP_PORT_MIN must be < RTP_PORT_MAX") {
		t.Fatalf("validation error = %q, want min < max problem", err)
	}
}
