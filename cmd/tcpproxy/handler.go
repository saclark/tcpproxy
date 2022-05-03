package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"time"
)

type LoadBalancer interface {
	Send(f func(addr string) error) error
}

type ConnHandler interface {
	ServeConn(conn net.Conn)
}

type ProxyDispatchHandler struct {
	Network          string
	DialTimeout      time.Duration
	portRoutingTable map[int]LoadBalancer
	// Poor man's mutex so we don't have to worry about not copying sync.Mutex.
	lock chan struct{}
}

func NewProxyDispatchHandler(network string, dialTimeout time.Duration, portRoutingTable map[int]LoadBalancer) *ProxyDispatchHandler {
	lock := make(chan struct{}, 1)
	lock <- struct{}{}
	return &ProxyDispatchHandler{
		Network:          network,
		DialTimeout:      dialTimeout,
		portRoutingTable: portRoutingTable,
		lock:             lock,
	}
}

func (h ProxyDispatchHandler) ServeConn(conn net.Conn) {
	defer conn.Close()

	addr := conn.LocalAddr().String()
	addrp, err := netip.ParseAddrPort(addr)
	if err != nil {
		log.Printf("ERROR: parsing address port: %v", err)
		return
	}

	<-h.lock
	lb, exists := h.portRoutingTable[int(addrp.Port())]
	h.lock <- struct{}{}
	if !exists {
		return
	}

	err = proxyConn(conn, h.Network, h.DialTimeout, lb)
	if err != nil {
		log.Printf("ERROR: proxying connection: %v", err)
	}
}

type ProxyHandler struct {
	Network     string
	DialTimeout time.Duration
	lb          LoadBalancer
}

func NewProxyHandler(network string, dialTimeout time.Duration, lb LoadBalancer) *ProxyHandler {
	return &ProxyHandler{
		Network:     network,
		DialTimeout: dialTimeout,
		lb:          lb,
	}
}

func (h ProxyHandler) ServeConn(conn net.Conn) {
	defer conn.Close()
	err := proxyConn(conn, h.Network, h.DialTimeout, h.lb)
	if err != nil {
		log.Printf("ERROR: proxying connection: %v", err)
	}
}

func proxyConn(conn net.Conn, network string, dialTimeout time.Duration, lb LoadBalancer) error {
	return lb.Send(func(addr string) error {
		targetConn, err := net.DialTimeout(network, addr, dialTimeout)
		if err != nil {
			log.Printf("WARN: Failed to connect to %s. Trying another target.", addr)
			return SkipBackend
		}
		defer targetConn.Close()

		done := make(chan error)
		go func() {
			defer close(done)
			done <- copyConn(targetConn, conn)
		}()

		if err := copyConn(conn, targetConn); err != nil {
			return fmt.Errorf("copying remote conn to local: %w", err)
		}
		if err := <-done; err != nil {
			return fmt.Errorf("copying local conn to remote: %w", err)
		}

		return nil
	})
}

func copyConn(dst, src net.Conn) error {
	_, err := io.Copy(dst, src)
	if err != nil {
		return fmt.Errorf("copying connection: %w", err)
	}
	if dst, ok := dst.(*net.TCPConn); ok {
		if err := dst.CloseWrite(); err != nil {
			return fmt.Errorf("closing write side of connection: %w", err)
		}
	}
	return nil
}
