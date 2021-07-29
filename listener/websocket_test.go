package listener

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/snple/mqtt"
	"github.com/stretchr/testify/require"
)

func TestWsConnClose(t *testing.T) {
	r, _ := net.Pipe()
	ws := &wsConn{r, new(websocket.Conn)}
	err := ws.Close()
	require.NoError(t, err)
}

func TestNewWebsocket(t *testing.T) {
	l := NewWebsocket("t1", testPort, nil)
	require.Equal(t, "t1", l.id)
	require.Equal(t, testPort, l.address)
}

func BenchmarkNewWebsocket(b *testing.B) {
	for n := 0; n < b.N; n++ {
		NewWebsocket("t1", testPort, nil)
	}
}

func TestWebsocketID(t *testing.T) {
	l := NewWebsocket("t1", testPort, nil)
	require.Equal(t, "t1", l.ID())
}

func BenchmarkWebsocketID(b *testing.B) {
	l := NewWebsocket("t1", testPort, nil)
	for n := 0; n < b.N; n++ {
		l.ID()
	}
}

func TestWebsocketListen(t *testing.T) {
	l := NewWebsocket("t1", testPort, nil)
	require.Nil(t, l.listen)
	err := l.Listen(nil)
	require.NoError(t, err)
	require.NotNil(t, l.listen)
	l.Close(func(id string) {})
}

func TestWebsocketListenTLS(t *testing.T) {
	cert, err := tls.X509KeyPair(testCertificate, testPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	l := NewWebsocketWithTLS("t1", testPort, nil, cfg)
	err = l.Listen(nil)
	require.NoError(t, err)
	require.NotNil(t, l.httpServer.TLSConfig)
	l.httpServer.Close()
	l.listen.Close()
}

func TestWebsocketServeAndClose(t *testing.T) {
	l := NewWebsocket("t1", testPort, nil)
	err := l.Listen(nil)
	require.NoError(t, err)

	o := make(chan bool)
	go func(o chan bool) {
		l.Serve(MockEstablisher)
		o <- true
	}(o)
	time.Sleep(time.Millisecond)

	var closed bool
	l.Close(func(id string) {
		closed = true
	})
	require.Equal(t, true, closed)
	<-o
}

func TestWebsocketServeTLSAndClose(t *testing.T) {
	cert, err := tls.X509KeyPair(testCertificate, testPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	l := NewWebsocketWithTLS("t1", testPort, nil, cfg)

	err = l.Listen(nil)
	require.NoError(t, err)

	o := make(chan bool)
	go func(o chan bool) {
		l.Serve(MockEstablisher)
		o <- true
	}(o)

	time.Sleep(time.Millisecond)
	var closed bool
	l.Close(func(id string) {
		closed = true
	})
	require.Equal(t, true, closed)
}

func TestWebsocketUpgrade(t *testing.T) {
	l := NewWebsocket("t1", testPort, nil)
	l.Listen(nil)
	e := make(chan bool)
	l.establish = func(id string, c net.Conn, ac mqtt.Auth) error {
		e <- true
		return nil
	}
	s := httptest.NewServer(http.HandlerFunc(l.handler))
	u := "ws" + strings.TrimPrefix(s.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(u, nil)
	require.NoError(t, err)
	require.Equal(t, true, <-e)

	s.Close()
	ws.Close()

}
