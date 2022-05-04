package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/saclark/tcpproxy/pkg/config"
)

func main() {
	port := flag.Int("port", 0, "route all connections on configured ports to a single listener on this port")
	flag.Parse()

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
		log.Fatalf("FATAL: reading configuration: %v", err)
	}

	var proxy ProxyServer
	if *port == 0 {
		proxy = NewTCPProxy(cfg)
	} else {
		proxy = NewConnectionSteeringTCPProxy(cfg, *port)
		log.Printf("INFO: routing all connections on configured ports to listener on :%d", *port)
	}

	serving := make(chan struct{})
	go func() {
		defer close(serving)
		if err := proxy.ListenAndServe(ctx); err != nil && err != ErrServerClosed {
			log.Printf("ERROR: listening and serving: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			// Process killed, shutdown gracefully.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := proxy.Shutdown(ctx); err != nil {
				log.Printf("ERROR: shutting down: %v", err)
			}
			// Wait for ListenAndServe goroutine to exit.
			<-serving
			return
		case <-serving:
			// ListenAndServe failed.
			return
		}
	}
}

// newCancelableContext returns a context that gets canceled by a SIGINT
func newCancelableContext() context.Context {
	doneCh := make(chan os.Signal, 1)
	signal.Notify(doneCh, os.Interrupt, syscall.SIGTERM)

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		<-doneCh
		log.Println("signal recieved")
		cancel()
	}()

	return ctx
}
