package registration

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/protocol"
	bridgeruntime "github.com/jtcressy/openclaw-sip-voice/bridge/internal/runtime"
)

func TestStartWithoutCredentialsIsInertAndDegraded(t *testing.T) {
	cfg := parseConfig(t, []string{
		"SIP_BIND_ADDR=127.0.0.1:5060",
		"SIP_ADVERTISE_ADDR=127.0.0.1:5060",
	})
	state := bridgeruntime.NewState(cfg)
	factory := &fakeFactory{err: errors.New("factory should not be called")}
	manager := NewManager(cfg, factory, state)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start without credentials: %v", err)
	}
	if factory.calls != 0 {
		t.Fatalf("factory calls = %d, want 0", factory.calls)
	}

	snapshot := state.Snapshot()
	if snapshot.BridgeState != protocol.BridgeStateDegraded {
		t.Fatalf("bridge state = %q, want degraded", snapshot.BridgeState)
	}
	if snapshot.Registration.State != protocol.RegistrationStateUnregistered {
		t.Fatalf("registration state = %q, want unregistered", snapshot.Registration.State)
	}
	if snapshot.Registration.ReasonCode != "not_configured" {
		t.Fatalf("reason = %q, want not_configured", snapshot.Registration.ReasonCode)
	}
}

func TestStartWithCredentialsRegistersAndRunsQualifyLoop(t *testing.T) {
	cfg := credentialConfig(t)
	state := bridgeruntime.NewState(cfg)
	tx := newFakeTransaction()
	manager := NewManager(cfg, &fakeFactory{tx: tx}, state)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start with credentials: %v", err)
	}
	tx.waitQualifyStarted(t)

	snapshot := state.Snapshot()
	if snapshot.BridgeState != protocol.BridgeStateReady {
		t.Fatalf("bridge state = %q, want ready", snapshot.BridgeState)
	}
	if snapshot.Registration.State != protocol.RegistrationStateRegistered {
		t.Fatalf("registration state = %q, want registered", snapshot.Registration.State)
	}
	if tx.registerCalls != 1 {
		t.Fatalf("register calls = %d, want 1", tx.registerCalls)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatalf("stop manager: %v", err)
	}
}

func TestRegisterAuthFailureIsSanitized(t *testing.T) {
	cfg := credentialConfig(t)
	state := bridgeruntime.NewState(cfg)
	tx := newFakeTransaction()
	tx.registerErr = fakeStatusError{statusCode: 401, message: "401 bad credentials for alice super-secret 198.51.100.10 1234"}
	manager := NewManager(cfg, &fakeFactory{tx: tx}, state)

	err := manager.Start(context.Background())
	if err == nil {
		t.Fatal("start unexpectedly succeeded")
	}
	assertNoCredentialLeak(t, err.Error())

	snapshot := state.Snapshot()
	if snapshot.BridgeState != protocol.BridgeStateDegraded {
		t.Fatalf("bridge state = %q, want degraded", snapshot.BridgeState)
	}
	if snapshot.Registration.State != protocol.RegistrationStateError {
		t.Fatalf("registration state = %q, want error", snapshot.Registration.State)
	}
	if snapshot.Registration.ReasonCode != "auth_failed" {
		t.Fatalf("reason = %q, want auth_failed", snapshot.Registration.ReasonCode)
	}
	assertNoCredentialLeak(t, snapshot.Registration.Message)
}

func TestRegisterServerFailureIsSanitized(t *testing.T) {
	cfg := credentialConfig(t)
	state := bridgeruntime.NewState(cfg)
	tx := newFakeTransaction()
	tx.registerErr = fakeStatusError{statusCode: 503, message: "503 from 198.51.100.10 for alice super-secret"}
	manager := NewManager(cfg, &fakeFactory{tx: tx}, state)

	err := manager.Start(context.Background())
	if err == nil {
		t.Fatal("start unexpectedly succeeded")
	}
	assertNoCredentialLeak(t, err.Error())

	snapshot := state.Snapshot()
	if snapshot.Registration.State != protocol.RegistrationStateError {
		t.Fatalf("registration state = %q, want error", snapshot.Registration.State)
	}
	if snapshot.Registration.ReasonCode != "registrar_unavailable" {
		t.Fatalf("reason = %q, want registrar_unavailable", snapshot.Registration.ReasonCode)
	}
	assertNoCredentialLeak(t, snapshot.Registration.Message)
}

func TestStopUnregistersAndMarksUnregistered(t *testing.T) {
	cfg := credentialConfig(t)
	state := bridgeruntime.NewState(cfg)
	tx := newFakeTransaction()
	manager := NewManager(cfg, &fakeFactory{tx: tx}, state)

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("start with credentials: %v", err)
	}
	tx.waitQualifyStarted(t)

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Stop(stopCtx); err != nil {
		t.Fatalf("stop manager: %v", err)
	}

	if tx.unregisterCalls != 1 {
		t.Fatalf("unregister calls = %d, want 1", tx.unregisterCalls)
	}
	if !tx.qualifyCanceled {
		t.Fatal("qualify loop context was not canceled")
	}
	snapshot := state.Snapshot()
	if snapshot.BridgeState != protocol.BridgeStateDegraded {
		t.Fatalf("bridge state = %q, want degraded", snapshot.BridgeState)
	}
	if snapshot.Registration.State != protocol.RegistrationStateUnregistered {
		t.Fatalf("registration state = %q, want unregistered", snapshot.Registration.State)
	}
	if snapshot.Registration.ReasonCode != "stopped" {
		t.Fatalf("reason = %q, want stopped", snapshot.Registration.ReasonCode)
	}
}

func parseConfig(t *testing.T, environ []string) config.Config {
	t.Helper()
	cfg, err := config.ParseEnv(environ)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return cfg
}

func credentialConfig(t *testing.T) config.Config {
	t.Helper()
	return parseConfig(t, []string{
		"UNIFI_TALK_SIP_SERVER=198.51.100.10",
		"UNIFI_TALK_SIP_USERNAME=alice",
		"UNIFI_TALK_SIP_PASSWORD=super-secret",
		"UNIFI_TALK_SIP_EXTENSION=1234",
		"SIP_BIND_ADDR=127.0.0.1:5060",
		"SIP_ADVERTISE_ADDR=127.0.0.1:5060",
	})
}

func assertNoCredentialLeak(t *testing.T, value string) {
	t.Helper()
	for _, forbidden := range []string{"198.51.100.10", "alice", "super-secret", "1234"} {
		if strings.Contains(value, forbidden) {
			t.Fatalf("value leaked %q: %s", forbidden, value)
		}
	}
}

type fakeFactory struct {
	tx    *fakeTransaction
	err   error
	calls int
}

func (f *fakeFactory) NewRegisterTransaction(_ context.Context, _ Credentials) (Transaction, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.tx, nil
}

type fakeTransaction struct {
	mu              sync.Mutex
	registerErr     error
	qualifyErr      error
	unregisterErr   error
	registerCalls   int
	unregisterCalls int
	qualifyStarted  chan struct{}
	qualifyCanceled bool
}

func newFakeTransaction() *fakeTransaction {
	return &fakeTransaction{qualifyStarted: make(chan struct{})}
}

func (t *fakeTransaction) Register(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.registerCalls++
	return t.registerErr
}

func (t *fakeTransaction) QualifyLoop(ctx context.Context) error {
	close(t.qualifyStarted)
	if t.qualifyErr != nil {
		return t.qualifyErr
	}
	<-ctx.Done()

	t.mu.Lock()
	t.qualifyCanceled = true
	t.mu.Unlock()

	return ctx.Err()
}

func (t *fakeTransaction) Unregister(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.unregisterCalls++
	return t.unregisterErr
}

func (t *fakeTransaction) waitQualifyStarted(tb testing.TB) {
	tb.Helper()
	select {
	case <-t.qualifyStarted:
	case <-time.After(time.Second):
		tb.Fatal("qualify loop did not start")
	}
}

type fakeStatusError struct {
	statusCode int
	message    string
}

func (e fakeStatusError) Error() string {
	return e.message
}

func (e fakeStatusError) StatusCode() int {
	return e.statusCode
}
