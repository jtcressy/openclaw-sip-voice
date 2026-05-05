package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/callflow"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/health"
	bridgemedia "github.com/jtcressy/openclaw-sip-voice/bridge/internal/media"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/registration"
	bridgeruntime "github.com/jtcressy/openclaw-sip-voice/bridge/internal/runtime"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/sipua"
)

const bridgeVersion = "0.1.0"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.ParseEnv(os.Environ())
	if err != nil {
		logger.Error("invalid bridge configuration", "error", err, "config", cfg.RedactedValues())
		os.Exit(2)
	}
	logger.Info("loaded bridge configuration", "config", cfg.RedactedValues())

	stack, err := sipua.NewStack(cfg)
	if err != nil {
		logger.Error("failed to construct SIP runtime", "error", err)
		os.Exit(1)
	}
	defer stack.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	state := bridgeruntime.NewState(cfg)
	capabilities := bridgeCapabilities(cfg)
	protocolServer := protocol.NewServer(protocol.Options{
		BridgeID:         "bridge_local_runtime",
		BridgeVersion:    bridgeVersion,
		SnapshotProvider: state,
		Capabilities:     &capabilities,
	})
	var outboundDialer callflow.OutboundDialer
	if cfg.UniFiConfigured() {
		outboundDialer = callflow.DiagoDialer{
			Diago: stack.Diago,
			DiagoDialerOptions: callflow.DiagoDialerOptions{
				Server:    cfg.UniFiTalkSIPServer,
				Username:  cfg.UniFiTalkSIPUsername,
				Password:  cfg.UniFiTalkSIPPassword,
				Extension: cfg.UniFiTalkSIPExtension,
				Transport: cfg.SIPTransport,
			},
		}
	}
	callManager := callflow.NewManager(callflow.Options{
		State:          state,
		EventSink:      protocolServer,
		OutboundDialer: outboundDialer,
		MediaFactory: bridgemedia.Factory{
			EventSink: protocolServer,
		},
	})
	protocolServer.SetCommandHandler(callManager)

	if cfg.UniFiConfigured() {
		if err := stack.Diago.ServeBackground(ctx, callManager.HandleDiagoInbound); err != nil {
			logger.Error("failed to start SIP listener", "error", err)
			os.Exit(1)
		}
	}

	registrationManager := registration.NewManager(cfg, registration.DiagoFactory{Diago: stack.Diago}, state)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := registrationManager.Stop(shutdownCtx); err != nil {
			logger.Warn("SIP registration stop did not complete cleanly", "error", err)
		}
	}()

	wsServer := &http.Server{
		Addr:              cfg.BridgeWSAddr.String(),
		Handler:           protocol.NewWebSocketHandler(protocolServer),
		ReadHeaderTimeout: 5 * time.Second,
	}
	healthServer := &http.Server{
		Addr:              cfg.MetricsAddr.String(),
		Handler:           health.NewHandler(state),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 2)
	go serveHTTP(errCh, logger, "protocol_websocket", wsServer)
	go serveHTTP(errCh, logger, "metrics_health", healthServer)
	go func() {
		if err := registrationManager.Start(ctx); err != nil {
			logger.Warn("SIP registration is not active", "error", err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("bridge shutdown requested")
	case err := <-errCh:
		logger.Error("bridge server stopped", "error", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = wsServer.Shutdown(shutdownCtx)
	_ = healthServer.Shutdown(shutdownCtx)
}

func serveHTTP(errCh chan<- error, logger *slog.Logger, name string, server *http.Server) {
	logger.Info("starting bridge server", "name", name, "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}

func bridgeCapabilities(cfg config.Config) protocol.Capabilities {
	if !cfg.UniFiConfigured() {
		return protocol.Capabilities{}
	}
	return protocol.MediaCapabilities()
}
