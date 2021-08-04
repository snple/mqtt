package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/logrusorgru/aurora"
	"github.com/snple/mqtt"
	"github.com/snple/mqtt/listener"
	"github.com/snple/mqtt/packets"
)

func main() {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	fmt.Println(aurora.Magenta("MQTT Server initializing..."), aurora.Cyan("TCP"))

	server := mqtt.New()

	server.SetHook(&MyHook{})

	tcp := listener.NewTCP("t1", ":1883", &mqtt.AuthAllow{})

	err := server.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := server.Serve()
		if err != nil {
			log.Fatal(err)
		}
	}()

	fmt.Println(aurora.BgMagenta("  Started!  "))

	<-done
	fmt.Println(aurora.BgRed("  Caught Signal  "))

	server.Close()
	fmt.Println(aurora.BgGreen("  Finished  "))
}

type MyHook struct {
}

var _ mqtt.Hook = (*MyHook)(nil)

func (*MyHook) Connect(_ *mqtt.Server, client *mqtt.Client) bool {
	log.Printf("Client connect: %v, addr: %v", client.ID, client.RemoteAddr())

	return true
}

func (*MyHook) DisConnect(_ *mqtt.Server, client *mqtt.Client, err error) {
	log.Printf("Client disconnect: %v, addr: %v", client.ID, client.RemoteAddr())
	if err != nil {
		log.Printf("Client disconnect: %v, err: %v", client.ID, err)
	}
}

func (*MyHook) Recv(_ *mqtt.Server, client *mqtt.Client, pk *packets.Packet) bool {
	switch pk.FixedHeader.Type {
	case packets.Subscribe:
		log.Printf("Client subscribe: %v, topic: %v", client.ID, pk.Topics)
	}
	return true
}

func (*MyHook) Send(_ *mqtt.Server, client *mqtt.Client, pk *packets.Packet) bool {
	switch pk.FixedHeader.Type {
	case packets.Suback:
		log.Printf("Client suback: %v, packet id: %v", client.ID, pk.PacketID)
	}
	return true
}

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

func (*MyHook) Push(_ *mqtt.Server, client *mqtt.Client, pk *packets.Packet) bool {
	log.Printf("Server push: %v, topic: %v, payload:%v", client.ID, pk.TopicName, pk.Payload)

	return true
}
