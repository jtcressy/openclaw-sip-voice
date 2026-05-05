package protocol

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"sync"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func NewWebSocketHandler(server *Server) http.Handler {
	return websocketHandler{server: server}
}

type websocketHandler struct {
	server *Server
}

func (h websocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	conn, rw, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return
	}

	transport := &websocketTransport{
		conn: conn,
		rw:   rw,
	}
	err = h.server.ServeSession(context.Background(), transport)
	if err != nil && !errors.Is(err, ErrUnsupportedProtocolVersion) {
		_ = conn.Close()
	}
}

type websocketTransport struct {
	conn net.Conn
	rw   *bufio.ReadWriter
	mu   sync.Mutex
}

func (t *websocketTransport) Send(_ context.Context, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := wsutil.WriteServerText(t.rw, payload); err != nil {
		return err
	}
	return t.rw.Flush()
}

func (t *websocketTransport) Receive(_ context.Context) ([]byte, error) {
	payload, err := wsutil.ReadClientText(t.rw)
	if flushErr := t.rw.Flush(); err == nil && flushErr != nil {
		return nil, flushErr
	}
	return payload, err
}

func (t *websocketTransport) Close() error {
	return t.conn.Close()
}
