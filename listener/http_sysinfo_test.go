package listener

import (
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snple/mqtt/system"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPStats(t *testing.T) {
	l := NewHTTPStats("t1", testPort)
	require.Equal(t, "t1", l.id)
	require.Equal(t, testPort, l.address)
}

func BenchmarkNewHTTPStats(b *testing.B) {
	for n := 0; n < b.N; n++ {
		NewHTTPStats("t1", testPort)
	}
}

func TestHTTPStatsID(t *testing.T) {
	l := NewHTTPStats("t1", testPort)
	require.Equal(t, "t1", l.ID())
}

func BenchmarkHTTPStatsID(b *testing.B) {
	l := NewHTTPStats("t1", testPort)
	for n := 0; n < b.N; n++ {
		l.ID()
	}
}

func TestHTTPStatsListen(t *testing.T) {
	l := NewHTTPStats("t1", testPort)
	err := l.Listen(new(system.Info))
	require.NoError(t, err)

	require.NotNil(t, l.system)
	require.NotNil(t, l.listen)
	require.Equal(t, testPort, l.httpServer.Addr)

	l.Close(func(id string) {})
}

func TestHTTPStatsListenTLS(t *testing.T) {
	cert, err := tls.X509KeyPair(testCertificate, testPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	l := NewHTTPStatsWithTLS("t1", testPort, cfg)
	err = l.Listen(new(system.Info))
	require.NoError(t, err)
	require.NotNil(t, l.httpServer.TLSConfig)
	l.Close(func(id string) {})
}

func TestHTTPStatsServeAndClose(t *testing.T) {
	l := NewHTTPStats("t1", testPort)
	err := l.Listen(&system.Info{
		Version: "test",
	})
	require.NoError(t, err)

	o := make(chan bool)
	go func(o chan bool) {
		l.Serve(MockEstablisher)
		o <- true
	}(o)
	time.Sleep(time.Millisecond)

	resp, err := http.Get("http://localhost" + testPort)
	require.NoError(t, err)
	require.NotNil(t, resp)

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)

	v := new(system.Info)
	err = json.Unmarshal(body, v)
	require.NoError(t, err)
	require.Equal(t, "test", v.Version)

	var closed bool
	l.Close(func(id string) {
		closed = true
	})
	require.Equal(t, true, closed)

	_, err = http.Get("http://localhost" + testPort)
	require.Error(t, err)

	<-o
}

func TestHTTPStatsServeTLSAndClose(t *testing.T) {
	cert, err := tls.X509KeyPair(testCertificate, testPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

	l := NewHTTPStatsWithTLS("t1", testPort, cfg)
	err = l.Listen(&system.Info{
		Version: "test",
	})
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

func TestHTTPStatsJSONHandler(t *testing.T) {
	l := NewHTTPStats("t1", testPort)
	err := l.Listen(&system.Info{
		Version: "test",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	l.jsonHandler(w, nil)
	resp := w.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	v := new(system.Info)
	err = json.Unmarshal(body, v)
	require.NoError(t, err)
	require.Equal(t, "test", v.Version)

	l.Close(func(id string) {})
}
