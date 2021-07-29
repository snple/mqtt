package listener

import (
	"crypto/tls"
	"net"
	"sync/atomic"

	"github.com/snple/mqtt"
	"github.com/snple/mqtt/system"
)

type TCP struct {
	id      string
	address string
	listen  net.Listener
	auth    mqtt.Auth
	tls     *tls.Config
	end     int64
}

// NewTCP initialises and returns a new TCP listener, listening on an address.
func NewTCP(id, address string, auth mqtt.Auth) *TCP {
	tcp := &TCP{
		id:      id,
		address: address,
		auth:    auth,
	}

	if tcp.auth == nil {
		tcp.auth = &mqtt.AuthAllow{}
	}

	return tcp
}

// NewTCP initialises and returns a new TCP listener, listening on an address.
func NewTCPWithTLS(id, address string, auth mqtt.Auth, tls *tls.Config) *TCP {
	tcp := &TCP{
		id:      id,
		address: address,
		auth:    auth,
		tls:     tls,
	}

	if tcp.auth == nil {
		tcp.auth = &mqtt.AuthAllow{}
	}

	return tcp
}

// ID returns the id of the listener.
func (l *TCP) ID() string {
	return l.id
}

// Auth returns the authentication controllers for the listener
func (l *TCP) Auth() mqtt.Auth {
	return l.auth
}

func (l *TCP) Listen(s *system.Info) error {
	var err error

	if l.tls != nil {
		l.listen, err = tls.Listen("tcp", l.address, l.tls)
	} else {
		l.listen, err = net.Listen("tcp", l.address)
	}

	return err
}

func (l *TCP) Serve(establish mqtt.EstablishFunc) error {
	for {
		if atomic.LoadInt64(&l.end) == 1 {
			return nil
		}

		conn, err := l.listen.Accept()
		if err != nil {
			return err
		}

		if atomic.LoadInt64(&l.end) == 0 {
			go establish(l.id, conn, l.auth)
		}
	}
}

// Close closes the listener and any client connections.
func (l *TCP) Close(closeClients mqtt.CloseFunc) {
	if atomic.LoadInt64(&l.end) == 0 {
		atomic.StoreInt64(&l.end, 1)
		closeClients(l.id)
	}

	if l.listen != nil {
		err := l.listen.Close()
		if err != nil {
			return
		}
	}
}
