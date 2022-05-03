package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/saclark/tcpproxy/pkg/config"
)

type ProxyServer interface {
	ListenAndServe(context.Context) error
	Shutdown(context.Context) error
}

type ConnectionSteeringTCPProxy struct {
	cfg  config.Config
	port int
	srv  *Server
}

func NewConnectionSteeringTCPProxy(cfg config.Config, port int) *ConnectionSteeringTCPProxy {
	return &ConnectionSteeringTCPProxy{
		cfg:  cfg,
		port: port,
	}
}

func (p *ConnectionSteeringTCPProxy) ListenAndServe(ctx context.Context) error {
	// Collect the ports and associate each with the appropriate targets.
	ports := []int{}
	portRoutingTable := map[int]LoadBalancer{}
	for _, app := range p.cfg.Apps {
		lb := NewRoundRobinLoadBalancer(app.Targets)
		for _, port := range app.Ports {
			ports = append(ports, port)
			portRoutingTable[port] = lb
		}
	}

	handler := NewProxyDispatchHandler("tcp", 3*time.Second, portRoutingTable)
	srv := NewServer(handler)
	p.srv = srv

	// Capture the underlying connection's file descriptor.
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

	l, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", p.port))
	if err != nil {
		return fmt.Errorf("starting listener: %w", err)
	}
	log.Printf("INFO: listening on %s", l.Addr())

	// Initialize the connection steering BPF program and maps.
	pdbpf, err := InitProxyDispatchBPF(<-sockfd, ports...)
	if err != nil {
		l.Close()
		return fmt.Errorf("initializing bpf: %w", err)
	}
	defer pdbpf.Close()

	// Serve.
	err = srv.Serve(l)
	log.Printf("INFO: shutdown listener on %s", l.Addr())
	return err
}

func (p *ConnectionSteeringTCPProxy) Shutdown(ctx context.Context) error {
	if p.srv == nil {
		return nil
	}
	return p.srv.Shutdown(ctx)
}

type TCPProxy struct {
	cfg  config.Config
	srvs map[string]*Server
}

func NewTCPProxy(cfg config.Config) *TCPProxy {
	return &TCPProxy{
		cfg:  cfg,
		srvs: map[string]*Server{},
	}
}

func (p *TCPProxy) ListenAndServe(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := errSlice{}

	for _, app := range p.cfg.Apps {
		lb := NewRoundRobinLoadBalancer(app.Targets)
		handler := NewProxyHandler("tcp", 3*time.Second, lb)
		srv := NewServer(handler)
		p.srvs[app.Name] = srv

		for _, port := range app.Ports {
			lc := net.ListenConfig{KeepAlive: 3 * time.Minute}
			l, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", port))
			if err != nil {
				return err
			}
			log.Printf("INFO: listening on %s", l.Addr())

			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := srv.Serve(l); err != nil && err != ErrServerClosed {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
				log.Printf("INFO: shutdown listener on %s", l.Addr())
			}()
		}
	}

	wg.Wait()
	if len(errs) > 0 {
		return errs
	}
	return ErrServerClosed
}

func (p *TCPProxy) Shutdown(ctx context.Context) error {
	errs := errSlice{}
	for _, srv := range p.srvs {
		if err := srv.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}
