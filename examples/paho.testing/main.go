package main

import (
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
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()

	fmt.Println(aurora.Magenta("MQTT Server initializing..."), aurora.Cyan("PAHO Testing Suite"))

	server := mqtt.New()
	tcp := listener.NewTCP("t1", ":1883", new(Auth))
	err := server.AddListener(tcp)
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

// Auth is an example auth provider for the server.
type Auth struct{}

// Auth returns true if a username and password are acceptable.
// Auth always returns true.
func (a *Auth) Auth(client *mqtt.Client) bool {
	return true
}

// ACL returns true if a user has access permissions to read or write on a topic.
// ACL is used to deny access to a specific topic to satisfy Test.test_subscribe_failure.
func (a *Auth) ACL(client *mqtt.Client, topic string, write bool) bool {
	if topic == "test/nosubscribe" {
		return false
	}
	return true
}
