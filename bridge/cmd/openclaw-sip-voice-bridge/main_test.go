package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestHealthcheckCommandSucceedsFor2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := runHealthcheckCommand([]string{"--url", server.URL + "/healthz", "--timeout", "1s"}); err != nil {
		t.Fatalf("healthcheck returned error: %v", err)
	}
}

func TestHealthcheckCommandFailsForNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := runHealthcheckCommand([]string{"--url", server.URL + "/readyz", "--timeout", "1s"}); err == nil {
		t.Fatal("healthcheck returned nil error, want non-2xx failure")
	}
}

func TestHealthcheckCommandFailsForTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := runHealthcheckCommand([]string{"--url", server.URL + "/healthz", "--timeout", "1ms"}); err == nil {
		t.Fatal("healthcheck returned nil error, want timeout failure")
	}
}

func TestHealthcheckCommandFailsForConnectionFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on local port: %v", err)
	}
	target := "http://" + listener.Addr().String() + "/healthz"
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	if err := runHealthcheckCommand([]string{"--url", target, "--timeout", "1s"}); err == nil {
		t.Fatal("healthcheck returned nil error, want connection failure")
	}
}

func TestHealthcheckCommandFailsForInvalidArgs(t *testing.T) {
	tests := [][]string{
		{"--timeout", "1s"},
		{"--url", "http://127.0.0.1:9078/healthz"},
		{"--url", "ftp://127.0.0.1/healthz", "--timeout", "1s"},
		{"--url", "http://127.0.0.1:9078/healthz", "--timeout", "1s", "extra"},
	}

	for _, args := range tests {
		if err := runHealthcheckCommand(args); err == nil {
			t.Fatalf("runHealthcheckCommand(%v) returned nil error, want invalid args failure", args)
		}
	}
	if err := runCommand([]string{"bogus"}); err == nil {
		t.Fatal("runCommand returned nil error, want unsupported subcommand failure")
	}
}
