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

// ConnectionSteeringTCPProxy listens on a single port and sets up a BPF socket
// steering program to route TCP connections from each of the configured ports
// to one of the port's configured backend targets in a load balanced manner.
// Unhealthy backends will be skipped until a healthy backend is found or all
// options have been exhausted.
type ConnectionSteeringTCPProxy struct {
	Port int
	cfg  config.Config
	srv  *Server
}

func NewConnectionSteeringTCPProxy(cfg config.Config, port int) *ConnectionSteeringTCPProxy {
	return &ConnectionSteeringTCPProxy{
		Port: port,
		cfg:  cfg,
	}
}

// ListenAndServe starts a TCP listener on p.Port, initializes the BPF socket
// steering program, and begins proxying connections.
//
// Accepted connections are configured to enable a 3 minute TCP keep-alive
// period and a 15 second dial timeout
//
// ListenAndServe always returns a non-nil error. After Shutdown, the returned
// error is ErrServerClosed.
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

	handler := NewProxyDispatchHandler("tcp", 15*time.Second, portRoutingTable)
	srv := NewServer(Recoverer(handler))
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

	l, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", p.Port))
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

	err = srv.Serve(l)
	log.Printf("INFO: shutdown listener on %s", l.Addr())
	return err
}

// Shutdown shuts down the server by calling p.srv.Shutdown and returning any
// error. See documentation for Server.Shutdown.
func (p *ConnectionSteeringTCPProxy) Shutdown(ctx context.Context) error {
	if p.srv == nil {
		return nil
	}
	return p.srv.Shutdown(ctx)
}

// TCPProxy listens on all of the configured ports and proxies TCP connections
// on each port to one of the port's configured backend targets in a load
// balanced manner. Unhealthy backends will be skipped until a healthy backend
// is found or all options have been exhausted.
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

// ListenAndServe starts a TCP listener for each configured port and begins
// proxying connections.
//
// Accepted connections are configured to enable a 3 minute TCP keep-alive
// period and a 15 second dial timeout
//
// ListenAndServe always returns a non-nil error. After Shutdown, the returned
// error is ErrServerClosed.
func (p *TCPProxy) ListenAndServe(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := errSlice{}

	for _, app := range p.cfg.Apps {
		lb := NewRoundRobinLoadBalancer(app.Targets)
		handler := NewProxyHandler("tcp", 15*time.Second, lb)
		srv := NewServer(Recoverer(handler))
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

// Shutdown shuts down by calling Shutdown on each server in p.srvs and
// returning any error. See documentation for Server.Shutdown.
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
