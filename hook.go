package mqtt

import (
	"github.com/snple/mqtt/packets"
)

// Hook is the server hook interface.
type Hook interface {
	// When the client connects to the server
	// If the return is false, the client will be rejected.
	Connect(*Server, *Client) bool

	// When the client disconnects
	DisConnect(*Server, *Client, error)

	// When the server receives a packet.
	// If the return is false, it will cancel the operation.
	Recv(*Server, *Client, *packets.Packet) bool

	// When the server sends a packet.
	// If the return is false, it will cancel the operation.
	Send(*Server, *Client, *packets.Packet) bool

	// When the server receives a message from the client publish.
	// If the return is false, it will cancel the operation.
	Emit(*Server, *Client, *packets.Packet) bool

	// When the server pushes a message to the client
	// If the return is false, it will cancel the operation.
	Push(*Server, *Client, *packets.Packet) bool
}

var _ Hook = (*NilHook)(nil)

type NilHook struct{}

func (*NilHook) Connect(*Server, *Client) bool {
	return true
}

func (*NilHook) DisConnect(*Server, *Client, error) {}

func (*NilHook) Recv(*Server, *Client, *packets.Packet) bool {
	return true
}

func (*NilHook) Send(*Server, *Client, *packets.Packet) bool {
	return true
}

func (*NilHook) Emit(*Server, *Client, *packets.Packet) bool {
	return true
}

func (*NilHook) Push(*Server, *Client, *packets.Packet) bool {
	return true
}
