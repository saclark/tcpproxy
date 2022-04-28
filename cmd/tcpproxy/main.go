package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/saclark/tcpproxy/pkg/config"
)

func main() {
	ctx := newCancelableContext()

	cfgStore := config.NewConfigStore("./config.json")

	// watch for changes to the config
	ch, err := cfgStore.StartWatcher()
	if err != nil {
		log.Fatalln(err)
	}
	defer cfgStore.Close()

	go func() {
		for cfg := range ch {
			fmt.Println("got config change:", cfg)
		}
	}()

	// TODO: put a proxy here :)
	cfg, err := cfgStore.Read()
	if err != nil {
		log.Fatalln(err)
	}

	for _, app := range cfg.Apps {
		lb := NewRoundRobinLoadBalancer(app.Targets)
		h := NewProxyHandler("tcp", 3*time.Second, lb)
		for _, port := range app.Ports {
			ln := Listener{
				KeepAlive: 3 * time.Minute,
				Handler:   h.HandleConn,
			}
			port := port // prevent loop variable capture
			go func() {
				err = ln.Listen(ctx, "tcp", fmt.Sprintf(":%d", port))
				if err != nil {
					log.Fatalln(err)
				}
			}()
		}
	}

	<-ctx.Done()
}

// newCancelableContext returns a context that gets canceled by a SIGINT
func newCancelableContext() context.Context {
	doneCh := make(chan os.Signal, 1)
	signal.Notify(doneCh, os.Interrupt)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		<-doneCh
		log.Println("signal recieved")
		cancel()
	}()

	return ctx
}
