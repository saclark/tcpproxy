package main

import "errors"

type RoundRobinLoadBalancer struct {
	backends []string
	next     chan string
	len      int
	i        int
}

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

var SkipBackend = errors.New("skip this backend")

var ErrNoHealthyBackends = errors.New("no healthy backends")

func (lb *RoundRobinLoadBalancer) Send(f func(addr string) error) error {
	var attemps int
	for attemps < lb.len {
		err := f(lb.nextAddr())
		switch err {
		case nil:
			return nil
		case SkipBackend:
			attemps++
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
