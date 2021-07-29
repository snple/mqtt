package listener

import (
	"crypto/tls"
	"errors"
	"log"
	"net"
	"testing"
	"time"

	"github.com/snple/mqtt"
	"github.com/stretchr/testify/require"
)

const (
	testPort = ":22222"
)

func TestNewTCP(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
	require.Equal(t, "t1", l.id)
	require.Equal(t, testPort, l.address)
}

func BenchmarkNewTCP(b *testing.B) {
	for n := 0; n < b.N; n++ {
		NewTCP("t1", testPort, nil)
	}
}

func TestTCPID(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
	require.Equal(t, "t1", l.ID())
}

func BenchmarkTCPID(b *testing.B) {
	l := NewTCP("t1", testPort, nil)
	for n := 0; n < b.N; n++ {
		l.ID()
	}
}

func TestTCPListen(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
	err := l.Listen(nil)
	require.NoError(t, err)

	l2 := NewTCP("t2", testPort, nil)
	err = l2.Listen(nil)
	require.Error(t, err)
	l.listen.Close()
}

func TestTCPListenTLS(t *testing.T) {
	cert, err := tls.X509KeyPair(testCertificate, testPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	l := NewTCPWithTLS("t1", testPort, nil, cfg)
	err = l.Listen(nil)
	require.NoError(t, err)
	l.listen.Close()
}

func TestTCPServeAndClose(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
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

// func TestTCPServeTLSAndClose(t *testing.T) {
// 	l := NewTCP("t1", testPort)
// 	l.SetConfig(&Config{
// 		Auth: new(auth.Allow),
// 		TLS: &TLS{
// 			Certificate: testCertificate,
// 			PrivateKey:  testPrivateKey,
// 		},
// 	})
// 	err := l.Listen(nil)
// 	require.NoError(t, err)

// 	o := make(chan bool)
// 	go func(o chan bool) {
// 		l.Serve(MockEstablisher)
// 		o <- true
// 	}(o)
// 	time.Sleep(time.Millisecond)
// 	var closed bool
// 	l.Close(func(id string) {
// 		closed = true
// 	})
// 	require.Equal(t, true, closed)
// 	<-o
// }

func TestTCPCloseError(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
	err := l.Listen(nil)
	require.NoError(t, err)
	o := make(chan bool)
	go func(o chan bool) {
		l.Serve(MockEstablisher)
		o <- true
	}(o)

	time.Sleep(time.Millisecond)
	l.listen.Close()
	l.Close(MockCloser)
	<-o
}

func TestTCPServeEnd(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
	err := l.Listen(nil)
	require.NoError(t, err)

	l.Close(MockCloser)
	l.Serve(func(id string, c net.Conn, ac mqtt.Auth) error {
		return nil
	})
}

func TestTCPEstablishThenError(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
	err := l.Listen(nil)
	require.NoError(t, err)

	o := make(chan bool)
	established := make(chan bool)
	go func() {
		l.Serve(func(id string, c net.Conn, ac mqtt.Auth) error {
			established <- true
			return errors.New("testing") // return an error to exit immediately
		})
		o <- true
	}()

	time.Sleep(time.Millisecond)
	net.Dial("tcp", l.listen.Addr().String())
	require.Equal(t, true, <-established)
	l.Close(MockCloser)
	<-o
}

func TestTCPEstablishButEnding(t *testing.T) {
	l := NewTCP("t1", testPort, nil)
	err := l.Listen(nil)
	require.NoError(t, err)
	l.end = 1

	o := make(chan bool)
	go func() {
		l.Serve(func(id string, c net.Conn, ac mqtt.Auth) error {
			return nil
		})
		o <- true
	}()

	net.Dial("tcp", l.listen.Addr().String())

	time.Sleep(time.Millisecond)
	l.Close(MockCloser)
	<-o

}
