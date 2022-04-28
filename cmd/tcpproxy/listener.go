package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

type Listener struct {
	KeepAlive time.Duration
	Handler   func(conn net.Conn)
}

func (l *Listener) Listen(ctx context.Context, network, address string) error {
	lc := net.ListenConfig{KeepAlive: l.KeepAlive}
	ln, err := lc.Listen(ctx, network, address)
	if err != nil {
		return fmt.Errorf("starting listener on %s: %w", address, err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				go log.Printf("Accepting conn: %v", err)
				continue
			}
		}
		go l.Handler(conn)
	}
}
