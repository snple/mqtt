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

	go func() {
		for {
			time.Sleep(time.Second * 10)

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
		}
	}()

	fmt.Println(aurora.BgMagenta("  Started!  "))

	<-done
	fmt.Println(aurora.BgRed("  Caught Signal  "))

	server.Close()
	fmt.Println(aurora.BgGreen("  Finished  "))
}
