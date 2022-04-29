package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

var ErrServerClosed = errors.New("proxy: Server closed")

var ErrServerRunning = errors.New("proxy: Server running")

type Server struct {
	Network     string
	Addr        string
	Handler     ConnHandler
	KeepAlive   time.Duration
	shutdownCtx context.Context
	cancel      context.CancelFunc
	done        chan error
}

func (s *Server) ListenAndServe() error {
	if !s.closed() {
		return ErrServerRunning
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan error)

	lc := net.ListenConfig{KeepAlive: s.KeepAlive}
	ln, err := lc.Listen(ctx, s.Network, s.Addr)
	if err != nil {
		return fmt.Errorf("starting listener on %s: %w", s.Addr, err)
	}

	var wg sync.WaitGroup

	go func() {
		// Await shutdown signal.
		<-ctx.Done()

		// Close the listener, breaking the Accept loop and returning from
		// ListenAndServe.
		err := ln.Close()

		// Wait for all handlers to complete or cancellation of shutdownCtx,
		// whichever comes first.
		handling := make(chan struct{})
		go func() {
			wg.Wait()
			close(handling)
		}()

		select {
		case <-s.shutdownCtx.Done():
		case <-handling:
		}

		s.done <- err
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				// Server shutdown.
				return ErrServerClosed
			default:
				go log.Printf("Accepting conn: %v", err)
				continue
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Handler.ServeConn(conn)
		}()
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.closed() {
		return ErrServerClosed
	}

	s.shutdownCtx = ctx

	s.cancel()
	s.cancel = nil

	err := <-s.done
	s.done = nil

	if err != nil {
		return err
	}

	return ctx.Err()
}

// TODO: Bug: May race if called simultaneously by multiple goroutines.
func (s *Server) closed() bool {
	return s.cancel == nil || s.done == nil
}
