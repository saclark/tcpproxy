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

	ln := Listener{
		KeepAlive: 3 * time.Minute,
		Handler:   handleConn,
	}

	go func() {
		err = ln.Listen(ctx, "tcp", fmt.Sprintf(":%d", cfg.Apps[0].Ports[0]))
		if err != nil {
			log.Fatalln(err)
		}
	}()

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
