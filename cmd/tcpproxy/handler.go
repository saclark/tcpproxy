package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

type LoadBalancer interface {
	SelectBackend() string
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

func (h ProxyHandler) HandleConn(conn net.Conn) {
	defer conn.Close()

	target := h.lb.SelectBackend()
	targetConn, err := net.DialTimeout(h.Network, target, h.DialTimeout)
	if err != nil {
		log.Printf("Failed to connect: '%v'. Choosing new target...", err)
		return
	}
	defer targetConn.Close()

	done := make(chan error)
	go func() {
		defer close(done)
		done <- copyConn(targetConn, conn)
	}()

	if err := copyConn(conn, targetConn); err != nil {
		log.Printf("Copying remote conn to local: %v", err)
	}
	if err := <-done; err != nil {
		log.Printf("Copying local conn to remote: %v", err)
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
