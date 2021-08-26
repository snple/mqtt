<img alt="Snple MQTT logo" src="docs/img/logo.png" width="200px">

[![PkgGoDev](https://pkg.go.dev/badge/github.com/snple/mqtt)](https://pkg.go.dev/github.com/snple/mqtt)

## Snple MQTT

[简体中文](README_zh.md)

Note: The API of this library is still unstable and has not been sufficiently tested, please do not use it in production environments.

#### Features
- MQTT 3.1.1 compatible.
- Full MQTT Feature-set (QoS, Retained, $SYS)
- Trie-based Subscription model.
- Ring Buffer packet codec.
- TCP, Websocket, (including SSL/TLS).
- Interfaces for Client Authentication and Topic access control.
- Bolt-backed persistence and storage interfaces.
- Server Publish (Publish, PublishToClientByID, ...).
- Event hooks (Recv, Send, ...), see `hook.go`.

#### Roadmap

- Improve event hooks and server publish interface
- MQTT v5 compatibility

#### Quick Start

``` go
import (
    "github.com/snple/mqtt"
    "log"
)

func main() {
    // Create the new MQTT Server.
    server := mqtt.New()

    // Create a TCP listener on a standard port.
    tcp := listener.NewTCP("t1", ":1883", &mqtt.AuthAllow{})

    // Add the listener to the server.
    err := server.AddListener(tcp)
    if err != nil {
        log.Fatal(err)
    }

    // Start the broker. Serve() is blocking - see examples folder
    // for usage ideas.
    err = server.Serve()
    if err != nil {
        log.Fatal(err)
    }
}
```

Examples of running the broker with various configurations can be found in the `examples` folder.

##### Authentication and ACL

Authentication and ACL may be configured on a per-listener basis by providing an Auth Controller to the listener configuration. Custom Auth Controllers should satisfy the `Auth` interface found in `auth.go`. Two default controllers are provided, `AuthAllow`, which allows all traffic, and `AuthDisallow`, which denies all traffic.

```go
    tcp := listener.NewTCP("t1", ":1883", &mqtt.AuthAllow{})
    err := server.AddListener(tcp)
```

> If no auth controller is provided in the listener configuration, the server will default to _Allowing_ all traffic.

##### SSL/TLS

SSL/TLS may be configured on both the TCP and Websocket listeners.

```go
    cert, err := tls.X509KeyPair(publicCertificate, privateKey)
    if err != nil {
        log.Fatal(err)
    }
    cfg := &tls.Config{Certificates: []tls.Certificate{cert}}

    tcp := listener.NewTCPWithTLS("t1", ":1883", &mqtt.AuthAllow{}, cfg)
    err := server.AddListener(tcp)
```
> Note the mandatory inclusion of the Auth Controller!

#### Data Persistence

Snple MQTT provides a `persistence.Store` interface for developing and attaching persistent stores to the broker. The default persistence mechanism packaged with the broker is backed by [Bolt](https://github.com/etcd-io/bbolt) and can be enabled by assigning a `*bolt.Store` to the server.

```go
    // import "github.com/snple/mqtt/persistence/bolt"
    err = server.AddStore(bolt.New("mqtt.db", nil))
    if err != nil {
        log.Fatal(err)
    }
```
> Persistence is on-demand (not flushed) and will potentially reduce throughput when compared to the standard in-memory store. Only use it if you need to maintain state through restarts.

#### Server publish

Snple MQTT provides interfaces such as `Publish`, `PublishToClientByID` etc. for publish messages directly from the server.

```go

    server.Publish(
        "time", // topic
        []byte(fmt.Sprintf(`{"time": "%s"}`, time.Now().Format(time.RFC3339))), // payload
        1,     // qos
        false, // retain
    )

    server.PublishToClientByID(
        "mqtt_123456", // client id
        "time",        // topic
        []byte(fmt.Sprintf(`{"time": "%s"}`, time.Now().Format(time.RFC3339))), // payload
        1,     // qos
        false, // retain
    )

```

With `PublishToClientByID`, you can publish messages to specified client, even if the client is not subscribed. (It depends on whether your client will handle unsubscribed messages.)

#### Server Hook interface

Snple MQTT provides a Hook interface for extending server functionality.

```go
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
```

With this interface, you can debug more easily, and:

```go
func (*MyHook) Emit(server *mqtt.Server, client *mqtt.Client, pk *packets.Packet) bool {
    log.Printf("Client publish: %v, topic: %v, payload:%v", client.ID, pk.TopicName, pk.Payload)

    if pk.TopicName == "time" {
        server.PublishToClientByID(
            client.ID,  // client id
            "time_ack", // topic
            []byte(fmt.Sprintf(`{"time": "%s"}`, time.Now().Format(time.RFC3339))), // payload
            1,     // qos
            false, // retain
        )
    }

    return true
}

```

This code demonstrates that when a client sends a message with `topic` of "time" to the server, the server gives direct feedback to the client.

## Contributions
Contributions and feedback are both welcomed and encouraged! Open an [issue](https://github.com/snple/mqtt/issues) to report a bug, ask a question, or make a feature request.
