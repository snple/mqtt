# Snple MQTT Broker

#### Features
- Paho MQTT 3.0 / 3.1.1 compatible.
- Full MQTT Feature-set (QoS, Retained, $SYS)
- Trie-based Subscription model.
- Ring Buffer packet codec.
- TCP, Websocket, (including SSL/TLS) and Dashboard listeners.
- Interfaces for Client Authentication and Topic access control.
- Bolt-backed persistence and storage interfaces.
- Event hooks (Recv, Send, ...), see `hook.go`.
- Publish interface (Publish, PublishToClient).

#### Roadmap

- Improve event hooks and publish interface
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

    // Add the listener to the server with default options (nil).
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

##### SSL

SSL may be configured on both the TCP and Websocket listeners by providing a public-private PEM key pair to the listener configuration as `[]byte` slices.
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

#### Paho Interoperability Test

You can check the broker against the [Paho Interoperability Test](https://github.com/eclipse/paho.mqtt.testing/tree/master/interoperability) by starting the broker using `examples/paho/main.go`, and then running the test with `python3 client_test.py` from the _interoperability_ folder.

## Contributions
Contributions and feedback are both welcomed and encouraged! Open an [issue](https://github.com/snple/mqtt/issues) to report a bug, ask a question, or make a feature request.
