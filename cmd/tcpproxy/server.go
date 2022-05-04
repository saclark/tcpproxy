package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// ErrServerClosed indicates that the server has been closed and should not be
// reused.
var ErrServerClosed = errors.New("server closed")

// Server serves network connections.
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

// Serve accepts incoming connections on the Listener l, creating a new service
// goroutine for each. The service goroutines pass connections to s.Handler for
// handling.
//
// Temporary errors from Accept will be retried with exponential backoff until
// success or delay has exceeded 1 second.
//
// Serve always returns a non-nil error. After Shutdown, the returned error is
// ErrServerClosed.
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

// Shutdown shuts down the server by closing all listeners, then waiting for all
// handlers to complete or for cancellation of ctx, whichever comes first.
//
// If the provided context expires before the shutdown is complete, Shutdown
// returns the context's error, otherwise it returns any error returned from
// closing the Server's underlying Listener(s).
//
// When Shutdown is called, Serve immediately returns ErrServerClosed. Make sure
// the program doesn't exit and waits instead for Shutdown to return.
//
// Once Shutdown has been called on a server, it should not be reused; future
// calls to Shutdown or Serve will return ErrServerClosed.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.closed() {
		return ErrServerClosed
	}

	// Mark the server as closed.
	s.cancel()
	<-s.ctx.Done()

	// Close the listeners, breaking the Accept loops and causing calls to Serve
	// to return.
	s.mu.Lock()
	errs := errSlice{}
	for ln := range s.listeners {
		if err := (*ln).Close(); err != nil {
			errs = append(errs, err)
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

	if ctx.Err() != nil {
		return ctx.Err()
	} else if len(errs) > 0 {
		return errs
	}
	return nil
}

func (s *Server) closed() bool {
	select {
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}

type errSlice []error

func (s errSlice) Error() string {
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s.Error()
	default:
		msgs := make([]string, len(s))
		for i, err := range s {
			msgs[i] = err.Error()
		}
		return fmt.Sprintf("%d errors: %s", len(s), strings.Join(msgs, "; "))
	}
}
