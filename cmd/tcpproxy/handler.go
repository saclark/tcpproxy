package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

type LoadBalancer interface {
	Send(f func(addr string) error) error
}

type ConnHandler interface {
	ServeConn(conn net.Conn)
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

	err := h.lb.Send(func(addr string) error {
		targetConn, err := net.DialTimeout(h.Network, addr, h.DialTimeout)
		if err != nil {
			// Log for debugging purposes.
			log.Printf("Failed to connect: '%v'. Choosing new target...", err)
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

	if err != nil {
		log.Println(err)
	}
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
