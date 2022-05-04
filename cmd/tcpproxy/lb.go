package main

import (
	"errors"
	"log"
	"net"
)

// RoundRobinLoadBalancer distributes requests to each backend in a set of
// badckends in a round robin fasion. It is safe for concurrent use.
type RoundRobinLoadBalancer struct {
	connect  func(address string) (net.Conn, error)
	backends []string
	next     chan string
	len      int
	i        int
}

// NewRoundRobinLoadBalancer constructs a new RoundRobinLoadBalancer. Any
// duplicate addresses will be deduplicated.
func NewRoundRobinLoadBalancer(addresses []string, connect func(address string) (net.Conn, error)) *RoundRobinLoadBalancer {
	backends := unique(addresses)
	len := len(backends)
	next := make(chan string, 1)
	if len > 0 {
		next <- backends[0]
	}
	return &RoundRobinLoadBalancer{
		connect:  connect,
		backends: backends,
		len:      len,
		next:     next,
	}
}

func unique(s []string) []string {
	uniq := []string{}
	m := map[string]struct{}{}
	for _, v := range s {
		if _, seen := m[v]; seen {
			continue
		}
		m[v] = struct{}{}
		uniq = append(uniq, v)
	}
	return uniq
}

// ErrNoHealthyBackends indicates all backends have been attempted and no more
// remain to be tried.
var ErrNoHealthyBackends = errors.New("no healthy backends")

// Connect attempts to connect to the next backend address in line. If a
// connection cannot be established, the next backend will be tried. If no
// successful connections can be made, ErrNoHealthyBackends is returned.
func (lb *RoundRobinLoadBalancer) Connect() (net.Conn, error) {
	var attempts int
	for attempts < lb.len {
		addr := lb.nextAddr()
		conn, err := lb.connect(addr)
		if err != nil {
			log.Printf("WARN: Failed to connect to %s. Trying another target.", addr)
			attempts++
		} else {
			return conn, nil
		}
	}
	return nil, ErrNoHealthyBackends
}

func (lb *RoundRobinLoadBalancer) nextAddr() string {
	addr := <-lb.next
	lb.i++
	lb.next <- lb.backends[lb.i%lb.len]
	return addr
}
