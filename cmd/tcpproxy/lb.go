package main

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

func (lb *RoundRobinLoadBalancer) SelectBackend() string {
	addr := <-lb.next
	lb.i++
	lb.next <- lb.backends[lb.i%lb.len]
	return addr
}
