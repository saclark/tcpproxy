package main

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

var ErrServerClosed = errors.New("server closed")

type Server struct {
	Handler   ConnHandler
	listeners map[*net.Listener]struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.Mutex
}

func NewServer(handler ConnHandler) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		Handler:   handler,
		listeners: map[*net.Listener]struct{}{},
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (s *Server) Serve(l net.Listener) error {
	defer s.removeListener(&l)
	s.mu.Lock()
	s.listeners[&l] = struct{}{}
	s.mu.Unlock()

	var tmpDelay time.Duration

	for {
		conn, err := l.Accept()
		if err != nil {
			if s.closed() {
				return ErrServerClosed
			}

			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tmpDelay == 0 {
					tmpDelay = 5 * time.Millisecond
				} else {
					tmpDelay *= 2
				}
				if max := 1 * time.Second; tmpDelay > max {
					tmpDelay = max
				}

				log.Printf("WARN: accept error: %v; retrying in %v", err, tmpDelay)
				time.Sleep(tmpDelay)

				continue
			}

			return err
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.Handler.ServeConn(conn)
		}()
	}
}

func (s *Server) removeListener(l *net.Listener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.listeners, l)
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.closed() {
		return ErrServerClosed
	}

	// Mark the server as closed.
	s.cancel()

	// Close the listeners, breaking the Accept loops and causing calls to Serve
	// to return.
	s.mu.Lock()
	var err error
	for ln := range s.listeners {
		if cerr := (*ln).Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	s.mu.Unlock()

	// Wait for all handlers to complete or for cancellation of ctx, whichever
	// comes first.
	handling := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(handling)
	}()

	select {
	case <-ctx.Done():
	case <-handling:
	}

	if err != nil {
		return err
	}

	return ctx.Err()
}

func (s *Server) closed() bool {
	select {
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}
