package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/logrusorgru/aurora"
	"go.etcd.io/bbolt"

	"github.com/snple/mqtt"
	"github.com/snple/mqtt/listener"
	"github.com/snple/mqtt/persistence/bolt"
)

func main() {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	fmt.Println(aurora.Magenta("MQTT Server initializing..."), aurora.Cyan("Persistence"))

	server := mqtt.New()
	tcp := listener.NewTCP("t1", ":1883", &mqtt.AuthAllow{})
	err := server.AddListener(tcp)
	if err != nil {
		log.Fatal(err)
	}

	err = server.SetStore(bolt.New("test.db", &bbolt.Options{
		Timeout: 500 * time.Millisecond,
	}))
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
