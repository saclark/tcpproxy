package main

import (
	"context"
	"fmt"
	"log"
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
	proxies := map[int]*Server{}
	for _, app := range cfg.Apps {
		lb := NewRoundRobinLoadBalancer(app.Targets)
		handler := NewProxyHandler("tcp", 3*time.Second, lb)
		for _, port := range app.Ports {
			s := &Server{
				Network:   "tcp",
				Addr:      fmt.Sprintf(":%d", port),
				Handler:   handler,
				KeepAlive: 3 * time.Minute,
			}

			proxies[port] = s

			wg.Add(1)
			go func() {
				defer wg.Done()
				log.Printf("Listening on %v", s.Addr)
				if err := s.ListenAndServe(); err != ErrServerClosed {
					log.Printf("ListenAndServe: %v", err)
				}
				log.Printf("Shutdown listener on %v", s.Addr)
			}()
		}
	}

	<-ctx.Done()
	for _, p := range proxies {
		// wrap in a func to accomodate defer
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := p.Shutdown(ctx); err != nil {
				log.Printf("Shutdown: %v", err)
			}
		}()
	}
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
