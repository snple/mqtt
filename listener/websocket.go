package listener

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/snple/mqtt"
	"github.com/snple/mqtt/system"
)

var (
	ErrInvalidMessage = errors.New("Message type not binary")

	// wsUpgrader is used to upgrade the incoming http/tcp connection to a
	// websocket compliant connection.
	wsUpgrader = &websocket.Upgrader{
		Subprotocols: []string{"mqtt"},
		CheckOrigin:  func(r *http.Request) bool { return true },
	}
)

// Websocket is a listener for establishing websocket connections.
type Websocket struct {
	id         string // the internal id of the listener.
	address    string // the network address to bind to.
	listen     net.Listener
	httpServer *http.Server // an http server for serving websocket connections.
	auth       mqtt.Auth
	tls        *tls.Config
	end        int64              // ensure the close methods are only called once.
	establish  mqtt.EstablishFunc // the server's establish conection handler.
}

// wsConn is a websocket connection which satisfies the net.Conn interface.
// Inspired by
type wsConn struct {
	net.Conn
	c *websocket.Conn
}

// Read reads the next span of bytes from the websocket connection and returns
// the number of bytes read.
func (ws *wsConn) Read(p []byte) (n int, err error) {
	op, r, err := ws.c.NextReader()
	if err != nil {
		return
	}

	if op != websocket.BinaryMessage {
		err = ErrInvalidMessage
		return
	}

	return r.Read(p)
}

// Write writes bytes to the websocket connection.
func (ws *wsConn) Write(p []byte) (n int, err error) {
	err = ws.c.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return
	}

	return len(p), nil
}

// Close signals the underlying websocket conn to close.
func (ws *wsConn) Close() error {
	return ws.Conn.Close()
}

// NewWebsocket initialises and returns a new Websocket listener, listening on an address.
func NewWebsocket(id, address string, auth mqtt.Auth) *Websocket {
	ws := &Websocket{
		id:      id,
		address: address,
		auth:    auth,
	}

	if ws.auth == nil {
		ws.auth = &mqtt.AuthAllow{}
	}

	return ws
}

// NewWebsocket initialises and returns a new Websocket listener, listening on an address.
func NewWebsocketWithTLS(id, address string, auth mqtt.Auth, tls *tls.Config) *Websocket {
	ws := &Websocket{
		id:      id,
		address: address,
		tls:     tls,
	}

	if auth == nil {
		ws.auth = &mqtt.AuthAllow{}
	}

	return ws
}

// ID returns the id of the listener.
func (l *Websocket) ID() string {
	return l.id
}

// Auth returns the authentication controllers for the listener
func (l *Websocket) Auth() mqtt.Auth {
	return l.auth
}

// Listen starts listening on the listener's network address.
func (l *Websocket) Listen(s *system.Info) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", l.handler)
	l.httpServer = &http.Server{
		Addr:      l.address,
		Handler:   mux,
		TLSConfig: l.tls,
	}

	ln, err := net.Listen("tcp", l.address)
	if err != nil {
		return err
	}

	l.listen = ln

	return nil
}

func (l *Websocket) handler(w http.ResponseWriter, r *http.Request) {
	c, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()

	l.establish(l.id, &wsConn{c.UnderlyingConn(), c}, l.auth)
}

// Serve starts waiting for new Websocket connections, and calls the connection
// establishment callback for any received.
func (l *Websocket) Serve(establish mqtt.EstablishFunc) error {
	l.establish = establish

	if l.tls != nil {
		return l.httpServer.ServeTLS(l.listen, "", "")
	}

	return l.httpServer.Serve(l.listen)
}

// Close closes the listener and any client connections.
func (l *Websocket) Close(closeClients mqtt.CloseFunc) {
	if atomic.LoadInt64(&l.end) == 0 {
		atomic.StoreInt64(&l.end, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.httpServer.Shutdown(ctx)
		l.listen.Close()
	}

	closeClients(l.id)
}
