package mqtt

import (
	"github.com/snple/mqtt/packets"
)

// Hook is the server hook interface.
type Hook interface {
	Accept(*Server, *Client) bool
	Remove(*Server, *Client, error)
	Recv(*Server, *Client, *packets.Packet) bool
	Send(*Server, *Client, *packets.Packet) bool
	Emit(*Server, *Client, *packets.Packet) bool
	Push(*Server, *Client, *packets.Packet) bool
}

var _ Hook = (*NilHook)(nil)

type NilHook struct{}

func (*NilHook) Accept(*Server, *Client) bool {
	return true
}

func (*NilHook) Remove(*Server, *Client, error) {}

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
