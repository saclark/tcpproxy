package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
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

	var wg sync.WaitGroup
	servers := map[string]*Server{}

	for _, app := range cfg.Apps {
		lb := NewRoundRobinLoadBalancer(app.Targets)
		handler := NewProxyHandler("tcp", 3*time.Second, lb)
		srv := NewServer(handler)

		servers[app.Name] = srv

		for _, port := range app.Ports {
			addr := fmt.Sprintf(":%d", port)
			log.Printf("Listening on %v", addr)

			lc := net.ListenConfig{KeepAlive: 3 * time.Minute}
			l, err := lc.Listen(ctx, "tcp", addr)
			if err != nil {
				defer l.Close() // In case we've already started listeners.
				log.Fatalln(err)
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := srv.Serve(l); err != nil && err != ErrServerClosed {
					log.Printf("Serve: %v", err)
				}
				log.Printf("Shutdown listener on %v", addr)
			}()
		}
	}

	<-ctx.Done()

	// Limit how long we will wait for all servers to shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("Shutdown: %v", err)
		}
	}

	// Wait for Serve goroutines to exit.
	wg.Wait()
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
