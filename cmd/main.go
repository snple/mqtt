package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/logrusorgru/aurora"
	"github.com/snple/mqtt"
	"github.com/snple/mqtt/listener"
)

func main() {
	tcpAddr := flag.String("tcp", ":1883", "network address for TCP listener")
	wsAddr := flag.String("ws", ":1882", "network address for Websocket listener")
	infoAddr := flag.String("info", ":8080", "network address for web info dashboard listener")
	flag.Parse()

	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	fmt.Println(aurora.Magenta("MQTT Broker initializing..."))
	fmt.Println(aurora.Cyan("TCP"), *tcpAddr)
	fmt.Println(aurora.Cyan("Websocket"), *wsAddr)
	fmt.Println(aurora.Cyan("$SYS Dashboard"), *infoAddr)

	server := mqtt.New()
	tcp := listener.NewTCP("t1", *tcpAddr, &mqtt.AuthAllow{})
	err := server.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}

	ws := listener.NewWebsocket("ws1", *wsAddr, &mqtt.AuthAllow{})
	err = server.AddListener(ws)
	if err != nil {
		log.Fatal(err)
	}

	stats := listener.NewHTTPStats("stats", *infoAddr)
	err = server.AddListener(stats)
	if err != nil {
		log.Fatal(err)
	}

	go server.Serve()
	fmt.Println(aurora.BgMagenta("  Started!  "))

	<-done
	fmt.Println(aurora.BgRed("  Caught Signal  "))

	server.Close()
	fmt.Println(aurora.BgGreen("  Finished  "))

}
