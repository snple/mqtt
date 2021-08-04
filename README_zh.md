<img alt="Snple MQTT logo" src="docs/img/logo.png" width="200px">

[![PkgGoDev](https://pkg.go.dev/badge/github.com/snple/mqtt)](https://pkg.go.dev/github.com/snple/mqtt)

## Snple MQTT

#### 功能概述
- 兼容 MQTT 3.1.1.
- 完整的 MQTT 功能集 (QoS, Retained, $SYS)
- 基于 Trie 的订阅模型.
- 基于环形缓冲区的数据包编解码器.
- TCP, Websocket, (包括 SSL/TLS).
- 客户端认证和 ACL.
- 基于 Bolt 的数据持久化和存储接口.
- 服务器发布 (Publish, PublishToClientByID, ...).
- 事件钩子 (Recv, Send, ...), 可查看 `hook.go`.

#### 路线图

- 改进事件钩子机制和服务器发布接口
- 支持 MQTT v5

#### 快速开始

``` go
import (
    "github.com/snple/mqtt"
    "log"
)

func main() {
    // 创建一个 MQTT 服务器.
    server := mqtt.New()

    // 在一个标准端口上创建一个 TCP 监听器.
    tcp := listener.NewTCP("t1", ":1883", &mqtt.AuthAllow{})

    // 添加监听器至服务器.
    err := server.AddListener(tcp)
    if err != nil {
        log.Fatal(err)
    }

    // 启动服务器. Serve() 函数是阻塞的 - 请参阅示例文件以了解使用方法
    err = server.Serve()
    if err != nil {
        log.Fatal(err)
    }
}
```

在 `examples` 文件夹中可以找到各种配置下运行服务器的例子。

##### 认证和 ACL

认证和 ACL 可以针对每个监听器配置。自定义的认证控制器应该满足 `auth.go` 中的 `Auth`。默认提供了两个控制器：
`AuthAllow` 用于所有流量, `AuthDisallow` 拒绝所有流量.

```go
    tcp := listener.NewTCP("t1", ":1883", &mqtt.AuthAllow{})
    err := server.AddListener(tcp)
```

> 如果没有为监听器配置认证控制器，服务器默认 _允许_ 所有流量.

##### SSL/TLS

TCP 和 Websocket 监听器够可以配置 SSL/TLS.

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

#### 数据持久化

Snple MQTT 提供了 `persistence.Store` 接口用于开发和附加数据持久化存储. 默认提供的持久化机制是使用 [Bolt](https://github.com/etcd-io/bbolt), 可以给服务器添加一个 `*bolt.Store` 来启用.

```go
    // import "github.com/snple/mqtt/persistence/bolt"
    err = server.AddStore(bolt.New("mqtt.db", nil))
    if err != nil {
        log.Fatal(err)
    }
```
> Persistence is on-demand (not flushed) and will potentially reduce throughput when compared to the standard in-memory store. Only use it if you need to maintain state through restarts.

> 与标准的内存存储相比，持久化有可能会降低吞吐量。仅用于你在重启时需要保存状态的时候.

#### 服务器发布

Snple MQTT 提供了 `Publish`, `PublishToClientByID` 等接口,用于从服务器直接发布消息.

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

使用 `PublishToClientByID`, 你可以将消息发布至指定客户端, 即使客户端未订阅. (就看你的客户端是否会处理未订阅的消息.)

#### 服务器 Hook 接口

Snple MQTT 提供了 Hook 接口用于扩展服务器功能.

```go
type Hook interface {
    // 当客户端连接到服务器
    // 如果返回 false， 客户端会被拒绝
    Connect(*Server, *Client) bool

    // 当客户端断开时
    DisConnect(*Server, *Client, error)

    // 当服务器收到一个数据包
    // 如果返回 false，该操作会被取消
    Recv(*Server, *Client, *packets.Packet) bool

    // 当服务器发送一个数据包
    // 如果返回 false，该操作会被取消
    Send(*Server, *Client, *packets.Packet) bool

    // 当服务器收到客户端发布的消息
    // 如果返回 false，该操作会被取消
    Emit(*Server, *Client, *packets.Packet) bool

    // 当服务器向客户端推送消息
    // 如果返回 false，该操作会被取消
    Push(*Server, *Client, *packets.Packet) bool
}
```

利用该接口, 你可以更方便地进行调试, 并且:

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

这段代码演示了, 当客户端向服务器发送 `topic` 为 “time” 的消息时, 服务器直接给该客户端一个反馈.

## 贡献

欢迎贡献和反馈! 打开一个 [issue](https://github.com/snple/mqtt/issues) 来报告错误，提出问题，或提出功能请求.
