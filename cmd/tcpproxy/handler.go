package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"time"
)

type ConnHandler interface {
	ServeConn(conn net.Conn)
}

type ConnHandlerFunc func(conn net.Conn)

func (f ConnHandlerFunc) ServeConn(conn net.Conn) {
	f(conn)
}

// Recoverer provides panic recovery middleware.
func Recoverer(next ConnHandler) ConnHandler {
	return ConnHandlerFunc(func(conn net.Conn) {
		defer func() {
			err := recover()
			if err != nil {
				log.Printf("ERROR: panic: %v", err)
			}
		}()
		next.ServeConn(conn)
	})
}

type LoadBalancer interface {
	Connect() (net.Conn, error)
}

// ProxyDispatchHandler proxies connections using the configured load balancer
// for the port on which they arrived, if one exists. Any errors are written
// back to the client connection.
type ProxyDispatchHandler struct {
	portRoutingTable map[int]LoadBalancer
	// Poor man's mutex so we aren't passing sync.Mutex by value to ServeConn.
	lock chan struct{}
}

func NewProxyDispatchHandler(portRoutingTable map[int]LoadBalancer) *ProxyDispatchHandler {
	lock := make(chan struct{}, 1)
	lock <- struct{}{}
	return &ProxyDispatchHandler{
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

	err = proxyConn(conn, lb)
	if err != nil {
		log.Printf("ERROR: proxying connection: %v", err)
	}
}

// ProxyHandler proxies connections using the provided load balancer. Any errors
// are written back to the client connection.
type ProxyHandler struct {
	lb LoadBalancer
}

func NewProxyHandler(lb LoadBalancer) *ProxyHandler {
	return &ProxyHandler{
		lb: lb,
	}
}

func (h ProxyHandler) ServeConn(conn net.Conn) {
	defer conn.Close()
	err := proxyConn(conn, h.lb)
	if err != nil {
		log.Printf("ERROR: proxying connection: %v", err)
	}
}

func proxyConn(conn net.Conn, lb LoadBalancer) error {
	targetConn, err := lb.Connect()
	if err != nil {
		writeConn(conn, []byte(fmt.Sprintf("ERROR: %v", err)))
		return err
	}
	defer targetConn.Close()

	done := make(chan error)
	go func() {
		defer close(done)
		done <- copyConn(targetConn, conn)
	}()

	if err := copyConn(conn, targetConn); err != nil {
		writeConn(conn, []byte(fmt.Sprintf("ERROR: %v", err)))
		return fmt.Errorf("copying remote conn to local: %w", err)
	}

	if err := <-done; err != nil {
		writeConn(conn, []byte(fmt.Sprintf("ERROR: %v", err)))
		return fmt.Errorf("copying local conn to remote: %w", err)
	}

	return nil
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

func writeConn(conn net.Conn, b []byte) error {
	deadline := time.Now().Add(3 * time.Second)
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return fmt.Errorf("setting write deadline: %w", err)
	}
	if _, err := conn.Write(b); err != nil {
		return fmt.Errorf("writing to conn: %w", err)
	}
	// Reset the deadline.
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("setting write deadline: %w", err)
	}
	return nil
}
