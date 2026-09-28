//go:build windows

package localipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/LexLuc/walkout/internal/transport/ndjson"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// DefaultTimeout leaves 25 ms of the 100 ms hook budget for adapter and host
// overhead. It covers connect, write, daemon processing, and response read.
const DefaultTimeout = 75 * time.Millisecond

var (
	ErrAlreadyRunning = errors.New("walkoutd is already running")
	ErrUnavailable    = errors.New("walkoutd is unavailable")
)

type Config struct {
	PipeName string
}

type Identity struct {
	SID                string
	PipeName           string
	SecurityDescriptor string
}

type Server struct {
	pipeName string
	handler  *ndjson.Handler
	listener net.Listener

	mu       sync.Mutex
	closed   bool
	serving  bool
	conns    map[net.Conn]struct{}
	closeCtx context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type Client struct {
	PipeName string
	Timeout  time.Duration
}

func CurrentUserIdentity() (Identity, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return Identity{}, fmt.Errorf("read current Windows identity: %w", err)
	}
	sid := user.User.Sid.String()
	if sid == "" {
		return Identity{}, errors.New("current Windows identity has no SID")
	}
	return Identity{
		SID:      sid,
		PipeName: `\\.\pipe\walkout-` + sid,
		SecurityDescriptor: fmt.Sprintf(
			"O:%sG:SYD:P(A;;GRGW;;;%s)(A;;GRGW;;;SY)",
			sid,
			sid,
		),
	}, nil
}

func NewServer(config Config, handler *ndjson.Handler) (*Server, error) {
	if handler == nil {
		return nil, errors.New("NDJSON handler is required")
	}
	identity, err := CurrentUserIdentity()
	if err != nil {
		return nil, err
	}
	pipeName := config.PipeName
	if pipeName == "" {
		pipeName = identity.PipeName
	}
	listener, err := winio.ListenPipe(pipeName, &winio.PipeConfig{
		SecurityDescriptor: identity.SecurityDescriptor,
		InputBufferSize:    ndjson.DefaultMaxRequestBytes,
		OutputBufferSize:   ndjson.DefaultMaxRequestBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: listen on named pipe: %v", ErrAlreadyRunning, err)
	}
	closeCtx, cancel := context.WithCancel(context.Background())
	return &Server{
		pipeName: pipeName,
		handler:  handler,
		listener: listener,
		conns:    make(map[net.Conn]struct{}),
		closeCtx: closeCtx,
		cancel:   cancel,
	}, nil
}

func (s *Server) PipeName() string {
	return s.pipeName
}

func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	if s.serving {
		s.mu.Unlock()
		return errors.New("named pipe server is already serving")
	}
	s.serving = true
	s.mu.Unlock()

	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept named pipe connection: %w", err)
		}
		if !s.track(conn) {
			_ = conn.Close()
			return nil
		}
		go s.serveConnection(conn)
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.cancel()
	connections := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		connections = append(connections, conn)
	}
	s.mu.Unlock()

	listenerErr := s.listener.Close()
	for _, conn := range connections {
		_ = conn.Close()
	}
	s.wg.Wait()
	if listenerErr != nil && !errors.Is(listenerErr, net.ErrClosed) {
		return fmt.Errorf("close named pipe listener: %w", listenerErr)
	}
	return nil
}

func (s *Server) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[conn] = struct{}{}
	// Add while holding the same lock Close uses before Wait. This prevents a
	// connection from being admitted between Close's snapshot and Wait.
	s.wg.Add(1)
	return true
}

func (s *Server) serveConnection(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()
	_ = s.handler.Serve(s.closeCtx, conn, conn)
}

func (c Client) Call(ctx context.Context, request ndjson.Request) (ndjson.Response, error) {
	if c.PipeName == "" {
		return ndjson.Response{}, errors.New("named pipe is required")
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return ndjson.Response{}, errors.New("named pipe timeout cannot be negative")
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := winio.DialPipeContext(callCtx, c.PipeName)
	if err != nil {
		return ndjson.Response{}, unavailable(callCtx, err)
	}
	defer conn.Close()
	if deadline, ok := callCtx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return ndjson.Response{}, unavailable(callCtx, err)
		}
	}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return ndjson.Response{}, unavailable(callCtx, err)
	}
	var response ndjson.Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return ndjson.Response{}, unavailable(callCtx, err)
	}
	if response.RequestID != request.RequestID {
		return ndjson.Response{}, fmt.Errorf(
			"named pipe response ID %q does not match request ID %q",
			response.RequestID,
			request.RequestID,
		)
	}
	return response, nil
}

func unavailable(ctx context.Context, cause error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, ctxErr)
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, cause)
}
