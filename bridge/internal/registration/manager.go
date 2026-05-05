package registration

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo/sip"

	"github.com/jtcressy/openclaw-sip-voice/bridge/internal/config"
)

const (
	DefaultExpiry = 5 * time.Minute
	label         = "default-line"
)

type Transaction interface {
	Register(context.Context) error
	QualifyLoop(context.Context) error
	Unregister(context.Context) error
}

type Factory interface {
	NewRegisterTransaction(context.Context, Credentials) (Transaction, error)
}

type State interface {
	SetRegistrationNotConfigured()
	SetRegistrationRegistering(label string)
	SetRegistrationRegistered(label string)
	SetRegistrationError(label string, reasonCode string, message string)
	SetRegistrationUnregistered(label string, reasonCode string, message string)
}

type Credentials struct {
	Server    string
	Username  string
	Password  string
	Extension string
}

func CredentialsFromConfig(cfg config.Config) Credentials {
	return Credentials{
		Server:    cfg.UniFiTalkSIPServer,
		Username:  cfg.UniFiTalkSIPUsername,
		Password:  cfg.UniFiTalkSIPPassword,
		Extension: cfg.UniFiTalkSIPExtension,
	}
}

func (c Credentials) Configured() bool {
	return c.Server != "" && c.Username != "" && c.Password != "" && c.Extension != ""
}

type DiagoFactory struct {
	Diago         *diago.Diago
	Expiry        time.Duration
	RetryInterval time.Duration
}

func (f DiagoFactory) NewRegisterTransaction(ctx context.Context, credentials Credentials) (Transaction, error) {
	if f.Diago == nil {
		return nil, errors.New("registration stack is not configured")
	}

	expiry := f.Expiry
	if expiry == 0 {
		expiry = DefaultExpiry
	}

	return f.Diago.RegisterTransaction(ctx, registrarURI(credentials), diago.RegisterOptions{
		Username:      credentials.Username,
		Password:      credentials.Password,
		Expiry:        expiry,
		RetryInterval: f.RetryInterval,
		AllowHeaders: []string{
			sip.INVITE.String(),
			sip.ACK.String(),
			sip.BYE.String(),
			sip.CANCEL.String(),
			sip.OPTIONS.String(),
		},
	})
}

type Manager struct {
	credentials Credentials
	factory     Factory
	state       State

	mu              sync.Mutex
	tx              Transaction
	registrationCtx context.Context
	cancel          context.CancelFunc
	refreshDone     chan struct{}
	refreshRunning  bool
}

func NewManager(cfg config.Config, factory Factory, state State) *Manager {
	return &Manager{
		credentials: CredentialsFromConfig(cfg),
		factory:     factory,
		state:       state,
	}
}

func (m *Manager) Register(ctx context.Context) error {
	if !m.credentials.Configured() {
		m.setNotConfigured()
		return nil
	}

	m.mu.Lock()
	if m.tx != nil {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	m.setRegistering()

	registrationCtx, cancel := context.WithCancel(ctx)
	if m.factory == nil {
		cancel()
		safeErr := newSafeError("registration_setup_failed", "SIP registration could not be prepared.", false)
		m.setError(safeErr)
		return safeErr
	}
	tx, err := m.factory.NewRegisterTransaction(registrationCtx, m.credentials)
	if err != nil {
		cancel()
		safeErr := newSafeError("registration_setup_failed", "SIP registration could not be prepared.", false)
		m.setError(safeErr)
		return safeErr
	}
	if tx == nil {
		cancel()
		safeErr := newSafeError("registration_setup_failed", "SIP registration could not be prepared.", false)
		m.setError(safeErr)
		return safeErr
	}
	if err := tx.Register(registrationCtx); err != nil {
		cancel()
		safeErr := safeErrorFor(err)
		m.setError(safeErr)
		return safeErr
	}

	m.mu.Lock()
	m.tx = tx
	m.registrationCtx = registrationCtx
	m.cancel = cancel
	m.mu.Unlock()

	m.setRegistered()
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	if err := m.Register(ctx); err != nil {
		return err
	}
	if !m.credentials.Configured() {
		return nil
	}

	m.mu.Lock()
	if m.tx == nil || m.refreshRunning {
		m.mu.Unlock()
		return nil
	}
	tx := m.tx
	registrationCtx := m.registrationCtx
	done := make(chan struct{})
	m.refreshDone = done
	m.refreshRunning = true
	m.mu.Unlock()

	go m.qualifyLoop(registrationCtx, tx, done)
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	tx := m.tx
	cancel := m.cancel
	done := m.refreshDone
	m.tx = nil
	m.registrationCtx = nil
	m.cancel = nil
	m.refreshDone = nil
	m.refreshRunning = false
	m.mu.Unlock()

	if !m.credentials.Configured() {
		m.setNotConfigured()
		return nil
	}

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			safeErr := safeErrorFor(ctx.Err())
			m.setError(safeErr)
			return safeErr
		}
	}

	if tx != nil {
		if err := tx.Unregister(ctx); err != nil {
			safeErr := newSafeError("unregister_failed", "SIP registration could not be stopped cleanly.", true)
			m.setError(safeErr)
			return safeErr
		}
	}

	m.setUnregistered("stopped", "SIP registration stopped.")
	return nil
}

func (m *Manager) qualifyLoop(ctx context.Context, tx Transaction, done chan<- struct{}) {
	defer close(done)

	err := tx.QualifyLoop(ctx)
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}

	safeErr := safeErrorFor(err)
	m.mu.Lock()
	if m.tx == tx {
		m.tx = nil
		m.registrationCtx = nil
		m.cancel = nil
		m.refreshDone = nil
		m.refreshRunning = false
	}
	m.mu.Unlock()
	m.setError(safeErr)
}

func (m *Manager) setNotConfigured() {
	if m.state != nil {
		m.state.SetRegistrationNotConfigured()
	}
}

func (m *Manager) setRegistering() {
	if m.state != nil {
		m.state.SetRegistrationRegistering(label)
	}
}

func (m *Manager) setRegistered() {
	if m.state != nil {
		m.state.SetRegistrationRegistered(label)
	}
}

func (m *Manager) setUnregistered(reasonCode string, message string) {
	if m.state != nil {
		m.state.SetRegistrationUnregistered(label, reasonCode, message)
	}
}

func (m *Manager) setError(err *SafeError) {
	if err == nil {
		err = newSafeError("registration_error", "SIP registration failed.", true)
	}
	if m.state != nil {
		m.state.SetRegistrationError(label, err.ReasonCode, err.Message)
	}
}

type SafeError struct {
	ReasonCode string
	Message    string
	Retryable  bool
}

func (e *SafeError) Error() string {
	return e.ReasonCode + ": " + e.Message
}

type statusCoder interface {
	StatusCode() int
}

func safeErrorFor(err error) *SafeError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return newSafeError("registration_canceled", "SIP registration was canceled.", true)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return newSafeError("registration_timeout", "SIP registration timed out.", true)
	}

	var status statusCoder
	if errors.As(err, &status) {
		switch code := status.StatusCode(); {
		case code == 401 || code == 403 || code == 407:
			return newSafeError("auth_failed", "SIP registration authentication failed.", false)
		case code >= 500 && code <= 599:
			return newSafeError("registrar_unavailable", "SIP registrar returned a server error.", true)
		case code >= 400 && code <= 499:
			return newSafeError("registration_rejected", "SIP registrar rejected the registration.", false)
		default:
			return newSafeError("registration_failed", "SIP registration failed.", true)
		}
	}

	return newSafeError("registration_failed", "SIP registration failed.", true)
}

func newSafeError(reasonCode string, message string, retryable bool) *SafeError {
	return &SafeError{
		ReasonCode: reasonCode,
		Message:    message,
		Retryable:  retryable,
	}
}

func registrarURI(credentials Credentials) sip.Uri {
	host, port := splitRegistrarHostPort(credentials.Server)
	return sip.Uri{
		Scheme:    "sip",
		User:      credentials.Extension,
		Host:      host,
		Port:      port,
		UriParams: sip.NewParams(),
	}
}

func splitRegistrarHostPort(value string) (string, int) {
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
