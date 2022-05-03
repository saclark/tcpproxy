package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/saclark/tcpproxy/pkg/config"
)

func main() {
	lport := flag.Int("port", 4001, "port on which to listen")
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

	ports := []int{}
	portRoutingTable := map[int]LoadBalancer{}
	for _, app := range cfg.Apps {
		lb := NewRoundRobinLoadBalancer(app.Targets)
		for _, port := range app.Ports {
			ports = append(ports, port)
			portRoutingTable[port] = lb
		}
	}

	sockfd := make(chan uintptr, 1)
	defer close(sockfd)

	lc := net.ListenConfig{
		KeepAlive: 3 * time.Minute,
		Control: func(_, _ string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				sockfd <- fd
			})
		},
	}

	l, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", *lport))
	if err != nil {
		log.Fatalf("FATAL: starting listener: %v", err)
	}
	log.Printf("INFO: listening on port %d", *lport)

	pdbpf, err := InitProxyDispatchBPF(<-sockfd, ports...)
	if err != nil {
		log.Fatalf("FATAL: initializing bpf: %v", err)
	}
	defer pdbpf.Close()

	handler := NewProxyDispatchHandler("tcp", 3*time.Second, portRoutingTable)
	srv := NewServer(handler)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(l); err != nil && err != ErrServerClosed {
			log.Printf("ERROR: serving: %v", err)
		}
		log.Println("INFO: shutdown listener")
	}()

	for {
		select {
		case <-ctx.Done():
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil && err != ErrServerClosed {
				log.Printf("ERROR: shutting down: %v", err)
			}
		case <-done:
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
