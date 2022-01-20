package mqtt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/xid"
	"github.com/snple/mqtt/circ"
	"github.com/snple/mqtt/packets"
	"github.com/snple/mqtt/system"
	"github.com/snple/mqtt/topics"
)

var (
	// defaultKeepalive is the default connection keepalive value in seconds.
	defaultKeepalive uint16 = 10

	ErrConnectionClosed = errors.New("Connection not open")
)

// Clients contains a map of the clients known by the broker.
type Clients struct {
	sync.RWMutex
	internal map[string]*Client // clients known by the broker, keyed on client id.
}

// New returns an instance of Clients.
func NewClients() *Clients {
	return &Clients{
		internal: make(map[string]*Client),
	}
}

// Add adds a new client to the clients map, keyed on client id.
func (c *Clients) Add(val *Client) {
	c.Lock()
	c.internal[val.ID] = val
	c.Unlock()
}

// Get returns the value of a client if it exists.
func (c *Clients) Get(id string) (*Client, bool) {
	c.RLock()
	val, ok := c.internal[id]
	c.RUnlock()
	return val, ok
}

// Len returns the length of the clients map.
func (c *Clients) Len() int {
	c.RLock()
	val := len(c.internal)
	c.RUnlock()
	return val
}

// Delete removes a client from the internal map.
func (c *Clients) Delete(id string) {
	c.Lock()
	delete(c.internal, id)
	c.Unlock()
}

// GetByListener returns clients matching a listener id.
func (c *Clients) GetByListener(id string) []*Client {
	clients := make([]*Client, 0, c.Len())
	c.RLock()
	for _, v := range c.internal {
		if v.Listener == id && atomic.LoadInt32(&v.State.Done) == 0 {
			clients = append(clients, v)
		}
	}
	c.RUnlock()
	return clients
}

// Client contains information about a client known by the broker.
type Client struct {
	sync.RWMutex
	conn          net.Conn             // the net.Conn used to establish the connection.
	r             *circ.Reader         // a reader for reading incoming bytes.
	w             *circ.Writer         // a writer for writing outgoing bytes.
	ID            string               // the client id.
	AC            Auth                 // an auth controller inherited from the listener.
	Subscriptions topics.Subscriptions // a map of the subscription filters a client maintains.
	Listener      string               // the id of the listener the client is connected to.
	Inflight      *Inflight            // a map of in-flight qos messages.
	Username      []byte               // the username the client authenticated with.
	Password      []byte               // the password the client authenticated with.
	keepalive     uint16               // the number of seconds the connection can wait.
	cleanSession  bool                 // indicates if the client expects a clean-session.
	packetID      uint32               // the current highest packetID.
	LWT           LWT                  // the last will and testament for the client.
	State         State                // the operational state of the client.
	system        *system.Info         // pointers to server system info.
}

// State tracks the state of the client.
type State struct {
	Done    int32           // atomic counter which indicates that the client has closed.
	started *sync.WaitGroup // tracks the goroutines which have been started.
	endedW  *sync.WaitGroup // tracks when the writer has ended.
	endedR  *sync.WaitGroup // tracks when the reader has ended.
	endOnce sync.Once       // only end once.
}

// NewClient returns a new instance of Client.
func NewClient(c net.Conn, r *circ.Reader, w *circ.Writer, s *system.Info) *Client {
	client := &Client{
		conn:          c,
		r:             r,
		w:             w,
		system:        s,
		keepalive:     defaultKeepalive,
		Inflight:      NewInflight(),
		Subscriptions: make(map[string]byte),
		State: State{
			started: new(sync.WaitGroup),
			endedW:  new(sync.WaitGroup),
			endedR:  new(sync.WaitGroup),
		},
	}

	client.refreshDeadline(client.keepalive)

	return client
}

// NewClientStub returns an instance of Client with basic initializations. This
// method is typically called by the persistence restoration system.
func NewClientStub(s *system.Info) *Client {
	return &Client{
		Inflight:      NewInflight(),
		Subscriptions: make(map[string]byte),
		State: State{
			Done: 1,
		},
	}
}

// Identify sets the identification values of a client instance.
func (c *Client) Identify(listenerID string, pk packets.Packet, ac Auth) {
	c.Listener = listenerID
	c.AC = ac

	c.ID = pk.ClientID
	if c.ID == "" {
		c.ID = xid.New().String()
	}

	c.r.ID = c.ID + " READER"
	c.w.ID = c.ID + " WRITER"

	c.Username = pk.Username
	c.Password = pk.Password
	c.cleanSession = pk.CleanSession
	c.keepalive = pk.Keepalive

	if pk.WillFlag {
		c.LWT = LWT{
			Topic:   pk.WillTopic,
			Message: pk.WillMessage,
			Qos:     pk.WillQos,
			Retain:  pk.WillRetain,
		}
	}

	c.refreshDeadline(c.keepalive)
}

// refreshDeadline refreshes the read/write deadline for the net.Conn connection.
func (c *Client) refreshDeadline(keepalive uint16) {
	if c.conn != nil {
		var expiry time.Time // Nil time can be used to disable deadline if keepalive = 0
		if keepalive > 0 {
			expiry = time.Now().Add(time.Duration(keepalive+(keepalive/2)) * time.Second)
		}
		c.conn.SetDeadline(expiry)
	}
}

// NextPacketID returns the next packet id for a client, looping back to 0
// if the maximum ID has been reached.
func (c *Client) NextPacketID() uint32 {
	i := atomic.LoadUint32(&c.packetID)
	if i == uint32(65535) || i == uint32(0) {
		atomic.StoreUint32(&c.packetID, 1)
		return 1
	}

	return atomic.AddUint32(&c.packetID, 1)
}

// NoteSubscription makes a note of a subscription for the client.
func (c *Client) NoteSubscription(filter string, qos byte) {
	c.Lock()
	c.Subscriptions[filter] = qos
	c.Unlock()
}

// ForgetSubscription forgests a subscription note for the client.
func (c *Client) ForgetSubscription(filter string) {
	c.Lock()
	delete(c.Subscriptions, filter)
	c.Unlock()
}

// Start begins the client goroutines reading and writing packets.
func (c *Client) Start() {
	c.State.started.Add(2)

	go func() {
		c.State.started.Done()
		c.w.WriteTo(c.conn)
		c.State.endedW.Done()
		c.Stop()
	}()
	c.State.endedW.Add(1)

	go func() {
		c.State.started.Done()
		c.r.ReadFrom(c.conn)
		c.State.endedR.Done()
		c.Stop()
	}()
	c.State.endedR.Add(1)

	c.State.started.Wait()
}

// Stop instructs the client to shut down all processing goroutines and disconnect.
func (c *Client) Stop() {
	if atomic.LoadInt32(&c.State.Done) == 1 {
		return
	}

	c.State.endOnce.Do(func() {
		c.r.Stop()
		c.w.Stop()
		c.State.endedW.Wait()

		c.conn.Close()

		c.State.endedR.Wait()
		atomic.StoreInt32(&c.State.Done, 1)
	})
}

// Read reads new packets from a client connection
func (c *Client) Read(h func(*Client, packets.Packet) error) error {
	for {
		if atomic.LoadInt32(&c.State.Done) == 1 && c.r.CapDelta() == 0 {
			return nil
		}

		c.refreshDeadline(c.keepalive)
		fh := new(packets.FixedHeader)
		err := c.ReadFixedHeader(fh)
		if err != nil {
			return err
		}

		pk, err := c.ReadPacket(fh)
		if err != nil {
			return err
		}

		err = h(c, pk) // Process inbound packet.
		if err != nil {
			return err
		}
	}
}

// readFixedHeader reads in the values of the next packet's fixed header.
func (c *Client) ReadFixedHeader(fh *packets.FixedHeader) error {
	p, err := c.r.Read(1)
	if err != nil {
		return err
	}

	err = fh.Decode(p[0])
	if err != nil {
		return err
	}

	// The remaining length value can be up to 5 bytes. Read through each byte
	// looking for continue values, and if found increase the read. Otherwise
	// decode the bytes that were legit.
	buf := make([]byte, 0, 6)
	i := 1
	n := 2
	for ; n < 6; n++ {
		p, err = c.r.Read(n)
		if err != nil {
			return err
		}

		buf = append(buf, p[i])

		// If it's not a continuation flag, end here.
		if p[i] < 128 {
			break
		}

		// If i has reached 4 without a length terminator, return a protocol violation.
		i++
		if i == 4 {
			return packets.ErrOversizedLengthIndicator
		}
	}

	// Calculate and store the remaining length of the packet payload.
	rem, _ := binary.Uvarint(buf)
	fh.Remaining = int(rem)

	// Having successfully read n bytes, commit the tail forward.
	c.r.CommitTail(n)
	atomic.AddInt32(&c.system.BytesRecv, int32(n))

	return nil
}

// ReadPacket reads the remaining buffer into an MQTT packet.
func (c *Client) ReadPacket(fh *packets.FixedHeader) (pk packets.Packet, err error) {
	atomic.AddInt32(&c.system.MessagesRecv, 1)

	pk.FixedHeader = *fh
	if pk.FixedHeader.Remaining == 0 {
		return
	}

	p, err := c.r.Read(pk.FixedHeader.Remaining)
	if err != nil {
		return pk, err
	}
	atomic.AddInt32(&c.system.BytesRecv, int32(len(p)))

	// Decode the remaining packet values using a fresh copy of the bytes,
	// otherwise the next packet will change the data of this one.
	px := append([]byte{}, p[:]...)

	switch pk.FixedHeader.Type {
	case packets.Connect:
		err = pk.ConnectDecode(px)
	case packets.Connack:
		err = pk.ConnackDecode(px)
	case packets.Publish:
		err = pk.PublishDecode(px)
		if err == nil {
			atomic.AddInt32(&c.system.PublishRecv, 1)
		}
	case packets.Puback:
		err = pk.PubackDecode(px)
	case packets.Pubrec:
		err = pk.PubrecDecode(px)
	case packets.Pubrel:
		err = pk.PubrelDecode(px)
	case packets.Pubcomp:
		err = pk.PubcompDecode(px)
	case packets.Subscribe:
		err = pk.SubscribeDecode(px)
	case packets.Suback:
		err = pk.SubackDecode(px)
	case packets.Unsubscribe:
		err = pk.UnsubscribeDecode(px)
	case packets.Unsuback:
		err = pk.UnsubackDecode(px)
	case packets.Pingreq:
	case packets.Pingresp:
	case packets.Disconnect:
	default:
		err = fmt.Errorf("No valid packet available; %v", pk.FixedHeader.Type)
	}

	c.r.CommitTail(pk.FixedHeader.Remaining)

	return
}

// WritePacket encodes and writes a packet to the client.
func (c *Client) WritePacket(pk packets.Packet) (n int, err error) {
	if atomic.LoadInt32(&c.State.Done) == 1 {
		return 0, ErrConnectionClosed
	}

	c.w.Mu.Lock()
	defer c.w.Mu.Unlock()

	buf := new(bytes.Buffer)
	switch pk.FixedHeader.Type {
	case packets.Connect:
		err = pk.ConnectEncode(buf)
	case packets.Connack:
		err = pk.ConnackEncode(buf)
	case packets.Publish:
		err = pk.PublishEncode(buf)
		if err == nil {
			atomic.AddInt32(&c.system.PublishSent, 1)
		}
	case packets.Puback:
		err = pk.PubackEncode(buf)
	case packets.Pubrec:
		err = pk.PubrecEncode(buf)
	case packets.Pubrel:
		err = pk.PubrelEncode(buf)
	case packets.Pubcomp:
		err = pk.PubcompEncode(buf)
	case packets.Subscribe:
		err = pk.SubscribeEncode(buf)
	case packets.Suback:
		err = pk.SubackEncode(buf)
	case packets.Unsubscribe:
		err = pk.UnsubscribeEncode(buf)
	case packets.Unsuback:
		err = pk.UnsubackEncode(buf)
	case packets.Pingreq:
		err = pk.PingreqEncode(buf)
	case packets.Pingresp:
		err = pk.PingrespEncode(buf)
	case packets.Disconnect:
		err = pk.DisconnectEncode(buf)
	default:
		err = fmt.Errorf("No valid packet available; %v", pk.FixedHeader.Type)
	}
	if err != nil {
		return
	}

	n, err = c.w.Write(buf.Bytes())
	if err != nil {
		return
	}
	atomic.AddInt32(&c.system.BytesSent, int32(n))
	atomic.AddInt32(&c.system.MessagesSent, 1)

	c.refreshDeadline(c.keepalive)

	return
}

// LocalAddr returns the local network address.
func (c *Client) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address.
func (c *Client) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// LWT contains the last will and testament details for a client connection.
type LWT struct {
	Topic   string // the topic the will message shall be sent to.
	Message []byte // the message that shall be sent when the client disconnects.
	Qos     byte   // the quality of service desired.
	Retain  bool   // indicates whether the will message should be retained
}

// InflightMessage contains data about a packet which is currently in-flight.
type InflightMessage struct {
	Packet  packets.Packet // the packet currently in-flight.
	Sent    int64          // the last time the message was sent (for retries) in unixtime.
	Resends int            // the number of times the message was attempted to be sent.
}

// Inflight is a map of InflightMessage keyed on packet id.
type Inflight struct {
	sync.RWMutex
	internal map[uint16]InflightMessage // internal contains the inflight messages.
}

func NewInflight() *Inflight {
	return &Inflight{
		internal: make(map[uint16]InflightMessage),
	}
}

// Set stores the packet of an Inflight message, keyed on message id. Returns
// true if the inflight message was new.
func (i *Inflight) Set(key uint16, in InflightMessage) bool {
	i.Lock()
	_, ok := i.internal[key]
	i.internal[key] = in
	i.Unlock()
	return !ok
}

// Get returns the value of an in-flight message if it exists.
func (i *Inflight) Get(key uint16) (InflightMessage, bool) {
	i.RLock()
	val, ok := i.internal[key]
	i.RUnlock()
	return val, ok
}

// Len returns the size of the in-flight messages map.
func (i *Inflight) Len() int {
	i.RLock()
	v := len(i.internal)
	i.RUnlock()
	return v
}

// GetAll returns all the in-flight messages.
func (i *Inflight) GetAll() map[uint16]InflightMessage {
	i.RLock()
	defer i.RUnlock()
	return i.internal
}

// Delete removes an in-flight message from the map. Returns true if the
// message existed.
func (i *Inflight) Delete(key uint16) bool {
	i.Lock()
	_, ok := i.internal[key]
	delete(i.internal, key)
	i.Unlock()
	return ok
}
