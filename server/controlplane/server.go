package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/ailiheizi/restoreweave/client/command"
)

// Server is the restoreweaved control-plane listener. Each accepted
// connection is handled in its own goroutine: frames are read, envelopes are
// dispatched, and results are written back. Connection-level failures close
// only that connection.
type Server struct {
	dispatcher *Dispatcher
	listener   net.Listener
	socketPath string

	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	closed  bool
	wg      sync.WaitGroup
	onError func(error)
}

// ServerOption configures the control-plane server.
type ServerOption func(*Server)

// WithErrorHandler installs a hook for per-connection failures (for example a
// logger). The default hook discards errors.
func WithErrorHandler(fn func(error)) ServerOption {
	return func(s *Server) {
		if fn != nil {
			s.onError = fn
		}
	}
}

// NewServer creates the Unix listener at socketPath, reclaiming a stale
// socket file when nothing is listening on it.
func NewServer(dispatcher *Dispatcher, socketPath string, options ...ServerOption) (*Server, error) {
	if dispatcher == nil {
		return nil, fmt.Errorf("dispatcher is required")
	}
	if err := prepareSocketPath(socketPath); err != nil {
		return nil, err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	server := &Server{
		dispatcher: dispatcher,
		listener:   listener,
		socketPath: socketPath,
		conns:      make(map[net.Conn]struct{}),
	}
	for _, option := range options {
		option(server)
	}
	return server, nil
}

// SocketPath returns the listener path.
func (s *Server) SocketPath() string { return s.socketPath }

// Serve accepts connections until the context is cancelled or the server is
// closed. It returns nil on shutdown and an error on a fatal accept failure.
func (s *Server) Serve(ctx context.Context) error {
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Close the listener so a blocked Accept unblocks and Serve can
			// observe the cancellation.
			_ = s.listener.Close()
		case <-ctxDone:
		}
	}()
	defer close(ctxDone)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || s.isClosed() {
				return nil
			}
			return fmt.Errorf("accept on %s: %w", s.socketPath, err)
		}
		s.track(conn)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConnection(ctx, conn)
		}()
	}
}

// Close stops accepting connections, closes every active connection, and
// removes the socket file.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	err := s.listener.Close()
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	_ = os.Remove(s.socketPath)
	return err
}

func (s *Server) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *Server) track(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		_ = conn.Close()
		return
	}
	s.conns[conn] = struct{}{}
}

func (s *Server) untrack(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// handleConnection serves one client until a frame error, an unrecoverable
// write failure, or shutdown. Malformed envelopes receive a FAILED result
// instead of terminating the connection.
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.untrack(conn)
	defer conn.Close()
	for {
		payload, err := ReadFrame(conn, MaxFrameBytes)
		if err != nil {
			// A clean EOF is a normal client disconnect, not a failure.
			if s.onError != nil && !errors.Is(err, io.EOF) {
				s.onError(fmt.Errorf("connection %s: %w", conn.RemoteAddr(), err))
			}
			return
		}
		var env command.Envelope
		if err := json.Unmarshal(payload, &env); err != nil {
			result := failedRawResult(env, time.Now().UTC(), newReason(
				ReasonCodeInvalidRequest, "envelope is not valid JSON: "+err.Error()))
			if writeErr := writeResult(conn, result); writeErr != nil {
				return
			}
			continue
		}
		result := s.dispatcher.Handle(ctx, env)
		if err := writeResult(conn, result); err != nil {
			if s.onError != nil {
				s.onError(fmt.Errorf("connection %s: write result: %w", conn.RemoteAddr(), err))
			}
			return
		}
	}
}

func writeResult(conn net.Conn, result command.Result) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return WriteFrame(conn, payload)
}
