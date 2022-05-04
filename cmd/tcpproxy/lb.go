package main

import "errors"

// RoundRobinLoadBalancer distributes requests to each backend in a set of
// badckends in a round robin fasion. It is safe for concurrent use.
type RoundRobinLoadBalancer struct {
	backends []string
	next     chan string
	len      int
	i        int
}

// NewRoundRobinLoadBalancer constructs a new RoundRobinLoadBalancer. Any
// duplicate addresses will be deduplicated.
func NewRoundRobinLoadBalancer(addresses []string) *RoundRobinLoadBalancer {
	backends := unique(addresses)
	len := len(backends)
	next := make(chan string, 1)
	if len > 0 {
		next <- backends[0]
	}
	return &RoundRobinLoadBalancer{
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

// SkipBackend instructs Send to attempt a new backend.
var SkipBackend = errors.New("skip this backend")

// ErrNoHealthyBackends indicates all backends have been attempted and no more
// remain to be tried.
var ErrNoHealthyBackends = errors.New("no healthy backends")

// Send executes f with an address from the set of backends. If f returns
// SkipBackend then f will be called again with the next backend in the set.
// Send returns either the return value of f, if f returns nil or an error that
// is not SkipBackend, or ErrNoHealthyBackends if all backends have been tried
// and skipped.
func (lb *RoundRobinLoadBalancer) Send(f func(addr string) error) error {
	var attempts int
	for attempts < lb.len {
		err := f(lb.nextAddr())
		switch err {
		case nil:
			return nil
		case SkipBackend:
			attempts++
		default:
			return err
		}
	}
	return ErrNoHealthyBackends
}

func (lb *RoundRobinLoadBalancer) nextAddr() string {
	addr := <-lb.next
	lb.i++
	lb.next <- lb.backends[lb.i%lb.len]
	return addr
}
