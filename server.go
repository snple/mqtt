// packet server provides a MQTT 3.1.1 compliant MQTT server.
package mqtt

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/snple/mqtt/circ"
	"github.com/snple/mqtt/packets"
	"github.com/snple/mqtt/persistence"
	"github.com/snple/mqtt/system"
	"github.com/snple/mqtt/topics"
)

const (
	Version = "1.0.0" // the server version.

	// maxPacketID is the maximum value of a 16-bit packet ID. If a
	// packet ID reaches this number, it resets to 0.
	maxPacketID = 65535
)

var (
	ErrListenerIDExists     = errors.New("Listener id already exists")
	ErrReadConnectInvalid   = errors.New("Connect packet was not valid")
	ErrConnectNotAuthorized = errors.New("Connect packet was not authorized")
	ErrInvalidTopic         = errors.New("Cannot publish to $ and $SYS topics")

	// SysTopicInterval is the number of milliseconds between $SYS topic publishes.
	SysTopicInterval time.Duration = 30000

	// inflightResendBackoff is a slice of seconds, which determines the
	// interval between inflight resend attempts.
	inflightResendBackoff = []int64{0, 1, 2, 10, 60, 120, 600, 3600, 21600}

	// inflightMaxResends is the maximum number of times to try resending QoS promises.
	inflightMaxResends = 6
)

// Server is an MQTT broker server. It should be created with server.New()
// in order to ensure all the internal fields are correctly populated.
type Server struct {
	done      chan bool         // indicate that the server is ending.
	Listeners *Listeners        // listeners are network interfaces which listen for new connections.
	Clients   *Clients          // clients which are known to the broker.
	Topics    *topics.Index     // an index of topic filter subscriptions and retained messages.
	System    *system.Info      // values about the server commonly found in $SYS topics.
	Store     persistence.Store // a persistent storage backend if desired.
	Hook      Hook
	sysTicker *time.Ticker // the interval ticker for sending updating $SYS topics.
}

// New returns a new instance of an MQTT broker.
func New() *Server {
	s := &Server{
		done:    make(chan bool),
		Clients: NewClients(),
		Topics:  topics.New(),
		System: &system.Info{
			Version: Version,
			Started: time.Now().Unix(),
		},
		Hook:      &NilHook{},
		sysTicker: time.NewTicker(SysTopicInterval * time.Millisecond),
	}

	// Expose server stats using the system listener so it can be used in the
	// dashboard and other more experimental listeners.
	s.Listeners = NewListeners(s.System)

	return s
}

// SetStore assigns a persistent storage backend to the server. This must be
// called before calling server.Server().
func (s *Server) SetStore(p persistence.Store) error {
	s.Store = p
	err := s.Store.Open()
	if err != nil {
		return err
	}

	return nil
}

func (s *Server) SetHook(h Hook) {
	s.Hook = h
}

// AddListener adds a new network listener to the server.
func (s *Server) AddListener(listener Listener) error {
	if _, ok := s.Listeners.Get(listener.ID()); ok {
		return ErrListenerIDExists
	}

	s.Listeners.Add(listener)
	err := listener.Listen(s.System)
	if err != nil {
		return err
	}

	return nil
}

// Serve starts the event loops responsible for establishing client connections
// on all attached listeners, and publishing the system topics.
func (s *Server) Serve() error {
	if s.Store != nil {
		err := s.readStore()
		if err != nil {
			return err
		}
	}

	go s.eventLoop()
	s.Listeners.ServeAll(s.EstablishConnection)
	s.publishSysTopics()

	return nil
}

// eventLoop loops forever, running various server processes at different intervals.
func (s *Server) eventLoop() {
	for {
		select {
		case <-s.done:
			s.sysTicker.Stop()
			return
		case <-s.sysTicker.C:
			s.publishSysTopics()
		}
	}
}

// EstablishConnection establishes a new client when a listener accepts a new
// connection.
func (s *Server) EstablishConnection(listenerID string, c net.Conn, auth Auth) error {
	client := NewClient(c,
		circ.NewReader(0, 0),
		circ.NewWriter(0, 0),
		s.System,
	)

	client.Start()

	fh := new(packets.FixedHeader)
	err := client.ReadFixedHeader(fh)
	if err != nil {
		return err
	}

	pk, err := client.ReadPacket(fh)
	if err != nil {
		return err
	}

	if pk.FixedHeader.Type != packets.Connect {
		return ErrReadConnectInvalid
	}

	client.Identify(listenerID, pk, auth)

	retcode, _ := pk.ConnectValidate()

	if retcode == packets.Accepted {
		if !auth.Auth(client) || !s.Hook.Accept(s, client) {
			retcode = packets.CodeConnectBadAuthValues
		}
	}

	if retcode != packets.Accepted {
		err = s.writeClient(client, packets.Packet{
			FixedHeader: packets.FixedHeader{
				Type: packets.Connack,
			},
			SessionPresent: false,
			ReturnCode:     retcode,
		})
		if err != nil {
			return err
		}

		s.closeClient(client, false)
		return nil
	}

	atomic.AddInt64(&s.System.ConnectionsTotal, 1)
	atomic.AddInt64(&s.System.ClientsConnected, 1)

	var sessionPresent bool
	if existing, ok := s.Clients.Get(pk.ClientID); ok {
		existing.Lock()
		if atomic.LoadInt32(&existing.State.Done) == 1 {
			atomic.AddInt64(&s.System.ClientsDisconnected, -1)
		}
		existing.Stop()
		if pk.CleanSession {
			for k := range existing.Subscriptions {
				delete(existing.Subscriptions, k)
				q := s.Topics.Unsubscribe(k, existing.ID)
				if q {
					atomic.AddInt64(&s.System.Subscriptions, -1)
				}
			}
		} else {
			client.Inflight = existing.Inflight // Inherit from existing session.
			client.Subscriptions = existing.Subscriptions
			sessionPresent = true
		}
		existing.Unlock()
	} else {
		atomic.AddInt64(&s.System.ClientsTotal, 1)
		if atomic.LoadInt64(&s.System.ClientsConnected) > atomic.LoadInt64(&s.System.ClientsMax) {
			atomic.AddInt64(&s.System.ClientsMax, 1)
		}
	}

	err = s.writeClient(client, packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Connack,
		},
		SessionPresent: sessionPresent,
		ReturnCode:     packets.Accepted,
	})
	if err != nil {
		return err
	}

	s.Clients.Add(client) // Overwrite any existing client with the same name.

	s.ResendClientInflight(client, true)

	if s.Store != nil {
		s.Store.WriteClient(persistence.Client{
			ID:       "cl_" + client.ID,
			ClientID: client.ID,
			T:        persistence.KClient,
			Listener: client.Listener,
			Username: client.Username,
			LWT:      persistence.LWT(client.LWT),
		})
	}

	err = client.Read(s.processPacket)
	if err != nil {
		s.Hook.Remove(s, client, err)
		s.closeClient(client, true)
	}

	atomic.AddInt64(&s.System.ClientsConnected, -1)
	atomic.AddInt64(&s.System.ClientsDisconnected, 1)

	return err
}

// writeClient writes packets to a client connection.
func (s *Server) writeClient(c *Client, pk packets.Packet) error {
	if s.Hook.Send(s, c, &pk) {
		_, err := c.WritePacket(pk)
		if err != nil {
			return err
		}
	}

	return nil
}

// processPacket processes an inbound packet for a client. Since the method is
// typically called as a goroutine, errors are primarily for test checking purposes.
func (s *Server) processPacket(c *Client, pk packets.Packet) error {
	if !s.Hook.Recv(s, c, &pk) {
		return nil
	}

	switch pk.FixedHeader.Type {
	case packets.Connect:
		return s.processConnect(c, pk)
	case packets.Disconnect:
		return s.processDisconnect(c, pk)
	case packets.Pingreq:
		return s.processPingreq(c, pk)
	case packets.Publish:
		r, err := pk.PublishValidate()
		if r != packets.Accepted {
			return err
		}
		return s.processPublish(c, pk)
	case packets.Puback:
		return s.processPuback(c, pk)
	case packets.Pubrec:
		return s.processPubrec(c, pk)
	case packets.Pubrel:
		return s.processPubrel(c, pk)
	case packets.Pubcomp:
		return s.processPubcomp(c, pk)
	case packets.Subscribe:
		r, err := pk.SubscribeValidate()
		if r != packets.Accepted {
			return err
		}
		return s.processSubscribe(c, pk)
	case packets.Unsubscribe:
		r, err := pk.UnsubscribeValidate()
		if r != packets.Accepted {
			return err
		}
		return s.processUnsubscribe(c, pk)
	default:
		return fmt.Errorf("No valid packet available; %v", pk.FixedHeader.Type)
	}
}

// processConnect processes a Connect packet. The packet cannot be used to
// establish a new connection on an existing connection. See EstablishConnection
// instead.
func (s *Server) processConnect(c *Client, pk packets.Packet) error {
	s.closeClient(c, true)
	return nil
}

// processDisconnect processes a Disconnect packet.
func (s *Server) processDisconnect(c *Client, pk packets.Packet) error {
	s.closeClient(c, false)
	return nil
}

// processPingreq processes a Pingreq packet.
func (s *Server) processPingreq(c *Client, pk packets.Packet) error {
	err := s.writeClient(c, packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Pingresp,
		},
	})
	if err != nil {
		return err
	}

	return nil
}

// processPublish processes a Publish packet.
func (s *Server) processPublish(c *Client, pk packets.Packet) error {
	if len(pk.TopicName) >= 4 && pk.TopicName[0:4] == "$SYS" {
		// Clients can't publish to $SYS topics.
		return nil
	}

	if !c.AC.ACL(c, pk.TopicName, true) {
		return nil
	}

	if pk.FixedHeader.Retain {
		out := pk.PublishCopy()
		q := s.Topics.RetainMessage(out)
		atomic.AddInt64(&s.System.Retained, q)
		if s.Store != nil {
			if q == 1 {
				s.Store.WriteRetained(persistence.Message{
					ID:          "ret_" + out.TopicName,
					T:           persistence.KRetained,
					FixedHeader: persistence.FixedHeader(out.FixedHeader),
					TopicName:   out.TopicName,
					Payload:     out.Payload,
				})
			} else {
				s.Store.DeleteRetained("ret_" + out.TopicName)
			}
		}
	}

	if pk.FixedHeader.Qos > 0 {
		ack := packets.Packet{
			FixedHeader: packets.FixedHeader{
				Type: packets.Puback,
			},
			PacketID: pk.PacketID,
		}

		if pk.FixedHeader.Qos == 2 {
			ack.FixedHeader.Type = packets.Pubrec
		}

		s.writeClient(c, ack)
		// omit errors in case of broken connection / LWT publish. ack send failures
		// will be handled by in-flight resending on next reconnect.
	}

	if !s.Hook.Emit(s, c, &pk) {
		return nil
	}

	s.publishToSubscribers(pk)

	return nil
}

// publishToSubscribers publishes a publish packet to all subscribers with
// matching topic filters.
func (s *Server) publishToSubscribers(pk packets.Packet) {
	subs := s.Topics.Subscribers(pk.TopicName)
	for id, qos := range subs {
		if c, ok := s.Clients.Get(id); ok {
			out := pk.PublishCopy()

			if qos > out.FixedHeader.Qos { // Inherit higher desired qos values.
				out.FixedHeader.Qos = qos
			}

			s.publishToClient(c, out)
		}
	}
}

func (s *Server) publishToClient(c *Client, out packets.Packet) {
	if !s.Hook.Push(s, c, &out) {
		return
	}

	if out.FixedHeader.Qos > 0 { // If QoS required, save to inflight index.
		if out.PacketID == 0 {
			out.PacketID = uint16(c.NextPacketID())
		}

		// If a message has a QoS, we need to ensure it is delivered to
		// the client at some point, one way or another. Store the publish
		// packet in the client's inflight queue and attempt to redeliver
		// if an appropriate ack is not received (or if the client is offline).
		sent := time.Now().Unix()
		q := c.Inflight.Set(out.PacketID, InflightMessage{
			Packet: out,
			Sent:   sent,
		})
		if q {
			atomic.AddInt64(&s.System.Inflight, 1)
		}

		if s.Store != nil {
			s.Store.WriteInflight(persistence.Message{
				ID:          "if_" + c.ID + "_" + strconv.Itoa(int(out.PacketID)),
				T:           persistence.KRetained,
				FixedHeader: persistence.FixedHeader(out.FixedHeader),
				TopicName:   out.TopicName,
				Payload:     out.Payload,
				Sent:        sent,
			})
		}
	}

	s.writeClient(c, out)
}

func (s *Server) Publish(topic string, payload []byte, qos byte, retain bool) error {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type:   packets.Publish,
			Retain: retain,
			Qos:    qos,
		},
		TopicName: topic,
		Payload:   payload,
	}

	if pk.FixedHeader.Retain {
		out := pk.PublishCopy()
		q := s.Topics.RetainMessage(out)
		atomic.AddInt64(&s.System.Retained, q)
		if s.Store != nil {
			if q == 1 {
				s.Store.WriteRetained(persistence.Message{
					ID:          "ret_" + out.TopicName,
					T:           persistence.KRetained,
					FixedHeader: persistence.FixedHeader(out.FixedHeader),
					TopicName:   out.TopicName,
					Payload:     out.Payload,
				})
			} else {
				s.Store.DeleteRetained("ret_" + out.TopicName)
			}
		}
	}

	s.publishToSubscribers(pk)

	return nil
}

func (s *Server) PublishToClient(id string, topic string, payload []byte, qos byte, retain bool) error {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type:   packets.Publish,
			Retain: retain,
			Qos:    qos,
		},
		TopicName: topic,
		Payload:   payload,
	}

	if c, ok := s.Clients.Get(id); ok {
		out := pk.PublishCopy()

		s.publishToClient(c, out)
	}

	return nil
}

// processPuback processes a Puback packet.
func (s *Server) processPuback(c *Client, pk packets.Packet) error {
	q := c.Inflight.Delete(pk.PacketID)
	if q {
		atomic.AddInt64(&s.System.Inflight, -1)
	}
	if s.Store != nil {
		s.Store.DeleteInflight("if_" + c.ID + "_" + strconv.Itoa(int(pk.PacketID)))
	}
	return nil
}

// processPubrec processes a Pubrec packet.
func (s *Server) processPubrec(c *Client, pk packets.Packet) error {
	out := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Pubrel,
			Qos:  1,
		},
		PacketID: pk.PacketID,
	}

	err := s.writeClient(c, out)
	if err != nil {
		return err
	}

	return nil
}

// processPubrel processes a Pubrel packet.
func (s *Server) processPubrel(c *Client, pk packets.Packet) error {
	out := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Pubcomp,
		},
		PacketID: pk.PacketID,
	}

	err := s.writeClient(c, out)
	if err != nil {
		return err
	}
	q := c.Inflight.Delete(pk.PacketID)
	if q {
		atomic.AddInt64(&s.System.Inflight, -1)
	}

	if s.Store != nil {
		s.Store.DeleteInflight("if_" + c.ID + "_" + strconv.Itoa(int(pk.PacketID)))
	}

	return nil
}

// processPubcomp processes a Pubcomp packet.
func (s *Server) processPubcomp(c *Client, pk packets.Packet) error {
	q := c.Inflight.Delete(pk.PacketID)
	if q {
		atomic.AddInt64(&s.System.Inflight, -1)
	}
	if s.Store != nil {
		s.Store.DeleteInflight("if_" + c.ID + "_" + strconv.Itoa(int(pk.PacketID)))
	}
	return nil
}

// processSubscribe processes a Subscribe packet.
func (s *Server) processSubscribe(c *Client, pk packets.Packet) error {
	retCodes := make([]byte, len(pk.Topics))
	for i := 0; i < len(pk.Topics); i++ {
		if !c.AC.ACL(c, pk.Topics[i], false) {
			retCodes[i] = packets.ErrSubAckNetworkError
		} else {
			q := s.Topics.Subscribe(pk.Topics[i], c.ID, pk.Qoss[i])
			if q {
				atomic.AddInt64(&s.System.Subscriptions, 1)
			}
			c.NoteSubscription(pk.Topics[i], pk.Qoss[i])
			retCodes[i] = pk.Qoss[i]

			if s.Store != nil {
				s.Store.WriteSubscription(persistence.Subscription{
					ID:     "sub_" + c.ID + ":" + pk.Topics[i],
					T:      persistence.KSubscription,
					Filter: pk.Topics[i],
					Client: c.ID,
					QoS:    pk.Qoss[i],
				})
			}
		}
	}

	err := s.writeClient(c, packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Suback,
		},
		PacketID:    pk.PacketID,
		ReturnCodes: retCodes,
	})
	if err != nil {
		return err
	}

	// Publish out any retained messages matching the subscription filter.
	for i := 0; i < len(pk.Topics); i++ {
		for _, pkv := range s.Topics.Messages(pk.Topics[i]) {
			s.writeClient(c, pkv) // omit errors, prefer continuing.
		}
	}

	return nil
}

// processUnsubscribe processes an unsubscribe packet.
func (s *Server) processUnsubscribe(c *Client, pk packets.Packet) error {
	for i := 0; i < len(pk.Topics); i++ {
		q := s.Topics.Unsubscribe(pk.Topics[i], c.ID)
		if q {
			atomic.AddInt64(&s.System.Subscriptions, -1)
		}
		c.ForgetSubscription(pk.Topics[i])
	}

	err := s.writeClient(c, packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type: packets.Unsuback,
		},
		PacketID: pk.PacketID,
	})
	if err != nil {
		return err
	}

	return nil
}

// publishSysTopics publishes the current values to the server $SYS topics.
// Due to the int to string conversions this method is not as cheap as
// some of the others so the publishing interval should be set appropriately.
func (s *Server) publishSysTopics() {
	pk := packets.Packet{
		FixedHeader: packets.FixedHeader{
			Type:   packets.Publish,
			Retain: true,
		},
	}

	s.System.Uptime = time.Now().Unix() - s.System.Started
	topics := map[string]string{
		"$SYS/broker/version":                   s.System.Version,
		"$SYS/broker/uptime":                    strconv.Itoa(int(s.System.Uptime)),
		"$SYS/broker/timestamp":                 strconv.Itoa(int(s.System.Started)),
		"$SYS/broker/load/bytes/received":       strconv.Itoa(int(s.System.BytesRecv)),
		"$SYS/broker/load/bytes/sent":           strconv.Itoa(int(s.System.BytesSent)),
		"$SYS/broker/clients/connected":         strconv.Itoa(int(s.System.ClientsConnected)),
		"$SYS/broker/clients/disconnected":      strconv.Itoa(int(s.System.ClientsDisconnected)),
		"$SYS/broker/clients/maximum":           strconv.Itoa(int(s.System.ClientsMax)),
		"$SYS/broker/clients/total":             strconv.Itoa(int(s.System.ClientsTotal)),
		"$SYS/broker/connections/total":         strconv.Itoa(int(s.System.ConnectionsTotal)),
		"$SYS/broker/messages/received":         strconv.Itoa(int(s.System.MessagesRecv)),
		"$SYS/broker/messages/sent":             strconv.Itoa(int(s.System.MessagesSent)),
		"$SYS/broker/messages/publish/dropped":  strconv.Itoa(int(s.System.PublishDropped)),
		"$SYS/broker/messages/publish/received": strconv.Itoa(int(s.System.PublishRecv)),
		"$SYS/broker/messages/publish/sent":     strconv.Itoa(int(s.System.PublishSent)),
		"$SYS/broker/messages/retained/count":   strconv.Itoa(int(s.System.Retained)),
		"$SYS/broker/messages/inflight":         strconv.Itoa(int(s.System.Inflight)),
		"$SYS/broker/subscriptions/count":       strconv.Itoa(int(s.System.Subscriptions)),
	}

	for topic, payload := range topics {
		pk.TopicName = topic
		pk.Payload = []byte(payload)
		q := s.Topics.RetainMessage(pk.PublishCopy())
		atomic.AddInt64(&s.System.Retained, q)
		s.publishToSubscribers(pk)
	}

	if s.Store != nil {
		s.Store.WriteServerInfo(persistence.ServerInfo{
			Info: *s.System,
			ID:   persistence.KServerInfo,
		})
	}
}

// ResendClientInflight attempts to resend all undelivered inflight messages
// to a
func (s *Server) ResendClientInflight(c *Client, force bool) error {
	if c.Inflight.Len() == 0 {
		return nil
	}

	nt := time.Now().Unix()
	for _, tk := range c.Inflight.GetAll() {
		if tk.Resends >= inflightMaxResends { // After a reasonable time, drop inflight packets.
			c.Inflight.Delete(tk.Packet.PacketID)
			if tk.Packet.FixedHeader.Type == packets.Publish {
				atomic.AddInt64(&s.System.PublishDropped, 1)
			}

			if s.Store != nil {
				s.Store.DeleteInflight("if_" + c.ID + "_" + strconv.Itoa(int(tk.Packet.PacketID)))
			}

			continue
		}

		// Only continue if the resend backoff time has passed and there's a backoff time.
		if !force && (nt-tk.Sent < inflightResendBackoff[tk.Resends] || len(inflightResendBackoff) < tk.Resends) {
			continue
		}

		if tk.Packet.FixedHeader.Type == packets.Publish {
			tk.Packet.FixedHeader.Dup = true
		}

		tk.Resends++
		tk.Sent = nt
		c.Inflight.Set(tk.Packet.PacketID, tk)
		_, err := c.WritePacket(tk.Packet)
		if err != nil {
			return err
		}

		if s.Store != nil {
			s.Store.WriteInflight(persistence.Message{
				ID:          "if_" + c.ID + "_" + strconv.Itoa(int(tk.Packet.PacketID)),
				T:           persistence.KRetained,
				FixedHeader: persistence.FixedHeader(tk.Packet.FixedHeader),
				TopicName:   tk.Packet.TopicName,
				Payload:     tk.Packet.Payload,
				Sent:        tk.Sent,
				Resends:     tk.Resends,
			})
		}
	}

	return nil
}

// Close attempts to gracefully shutdown the server, all listeners, clients, and stores.
func (s *Server) Close() error {
	close(s.done)
	s.Listeners.CloseAll(s.closeListenerClients)

	if s.Store != nil {
		s.Store.Close()
	}

	return nil
}

// closeListenerClients closes all clients on the specified listener.
func (s *Server) closeListenerClients(listener string) {
	clients := s.Clients.GetByListener(listener)
	for _, cl := range clients {
		s.closeClient(cl, false) // omit errors
	}

}

// closeClient closes a client connection and publishes any LWT messages.
func (s *Server) closeClient(c *Client, sendLWT bool) error {
	if sendLWT && c.LWT.Topic != "" {
		s.processPublish(c, packets.Packet{
			FixedHeader: packets.FixedHeader{
				Type:   packets.Publish,
				Retain: c.LWT.Retain,
				Qos:    c.LWT.Qos,
			},
			TopicName: c.LWT.Topic,
			Payload:   c.LWT.Message,
		})
	}

	c.Stop()

	return nil
}

// readStore reads in any data from the persistent datastore (if applicable).
func (s *Server) readStore() error {
	info, err := s.Store.ReadServerInfo()
	if err != nil {
		return fmt.Errorf("load server info; %w", err)
	}
	s.loadServerInfo(info)

	clients, err := s.Store.ReadClients()
	if err != nil {
		return fmt.Errorf("load clients; %w", err)
	}
	s.loadClients(clients)

	subs, err := s.Store.ReadSubscriptions()
	if err != nil {
		return fmt.Errorf("load subscriptions; %w", err)
	}
	s.loadSubscriptions(subs)

	inflight, err := s.Store.ReadInflight()
	if err != nil {
		return fmt.Errorf("load inflight; %w", err)
	}
	s.loadInflight(inflight)

	retained, err := s.Store.ReadRetained()
	if err != nil {
		return fmt.Errorf("load retained; %w", err)
	}
	s.loadRetained(retained)

	return nil
}

// loadServerInfo restores server info from the datastore.
func (s *Server) loadServerInfo(v persistence.ServerInfo) {
	version := s.System.Version
	s.System = &v.Info
	s.System.Version = version
}

// loadSubscriptions restores subscriptions from the datastore.
func (s *Server) loadSubscriptions(v []persistence.Subscription) {
	for _, sub := range v {
		s.Topics.Subscribe(sub.Filter, sub.Client, sub.QoS)
		if c, ok := s.Clients.Get(sub.Client); ok {
			c.NoteSubscription(sub.Filter, sub.QoS)
		}
	}
}

// loadClients restores clients from the datastore.
func (s *Server) loadClients(v []persistence.Client) {
	for _, c2 := range v {
		c := NewClientStub(s.System)
		c.ID = c2.ClientID
		c.Listener = c2.Listener
		c.Username = c2.Username
		c.LWT = LWT(c2.LWT)
		s.Clients.Add(c)
	}
}

// loadInflight restores inflight messages from the datastore.
func (s *Server) loadInflight(v []persistence.Message) {
	for _, msg := range v {
		if c, ok := s.Clients.Get(msg.Client); ok {
			c.Inflight.Set(msg.PacketID, InflightMessage{
				Packet: packets.Packet{
					FixedHeader: packets.FixedHeader(msg.FixedHeader),
					PacketID:    msg.PacketID,
					TopicName:   msg.TopicName,
					Payload:     msg.Payload,
				},
				Sent:    msg.Sent,
				Resends: msg.Resends,
			})
		}
	}
}

// loadRetained restores retained messages from the datastore.
func (s *Server) loadRetained(v []persistence.Message) {
	for _, msg := range v {
		s.Topics.RetainMessage(packets.Packet{
			FixedHeader: packets.FixedHeader(msg.FixedHeader),
			TopicName:   msg.TopicName,
			Payload:     msg.Payload,
		})
	}
}
