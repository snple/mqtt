package listener

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/snple/mqtt"
	"github.com/snple/mqtt/system"
)

// HTTPStats is a listener for presenting the server $SYS stats on a JSON http endpoint.
type HTTPStats struct {
	id         string       // the internal id of the listener.
	system     *system.Info // pointers to the server data.
	listen     net.Listener
	address    string       // the network address to bind to.
	httpServer *http.Server // the http server.
	tls        *tls.Config
	end        int32 // ensure the close methods are only called once.}
}

// NewHTTPStats initialises and returns a new HTTP listener, listening on an address.
func NewHTTPStats(id, address string) *HTTPStats {
	return &HTTPStats{
		id:      id,
		address: address,
	}
}

// NewHTTPStatsWithTLS initialises and returns a new HTTP listener, listening on an address.
func NewHTTPStatsWithTLS(id, address string, tls *tls.Config) *HTTPStats {
	return &HTTPStats{
		id:      id,
		address: address,
		tls:     tls,
	}
}

// ID returns the id of the listener.
func (l *HTTPStats) ID() string {
	return l.id
}

func (l *HTTPStats) Auth() mqtt.Auth {
	return &mqtt.AuthDisallow{}
}

// Listen starts listening on the listener's network address.
func (l *HTTPStats) Listen(s *system.Info) error {
	l.system = s
	mux := http.NewServeMux()
	mux.HandleFunc("/", l.jsonHandler)
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

// Serve starts listening for new connections and serving responses.
func (l *HTTPStats) Serve(establish mqtt.EstablishFunc) error {
	if l.tls != nil {
		return l.httpServer.ServeTLS(l.listen, "", "")
	} else {
		return l.httpServer.Serve(l.listen)
	}
}

// Close closes the listener and any client connections.
func (l *HTTPStats) Close(closeClients mqtt.CloseFunc) {
	if atomic.LoadInt32(&l.end) == 0 {
		atomic.StoreInt32(&l.end, 1)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.httpServer.Shutdown(ctx)
		l.listen.Close()
	}

	closeClients(l.id)
}

// jsonHandler is an HTTP handler which outputs the $SYS stats as JSON.
func (l *HTTPStats) jsonHandler(w http.ResponseWriter, req *http.Request) {
	info, err := json.MarshalIndent(l.system, "", "\t")
	if err != nil {
		io.WriteString(w, err.Error())
		return
	}

	w.Write(info)
}
