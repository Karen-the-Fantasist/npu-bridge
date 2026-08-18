package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"example.com/npu-bridge/internal/endpoint"
)

type ServerOptions struct {
	Listen                 endpoint.Endpoint
	Target                 endpoint.Endpoint
	WorkerPath             string
	ConnectTimeout         time.Duration
	AllowNonLoopbackListen bool
	AllowNonLoopbackTarget bool
	LogWriter              io.Writer
}

type Server struct {
	opts       ServerOptions
	listener   net.Listener
	cleanup    func() error
	closeOnce  sync.Once
	workers    sync.WaitGroup
	workerLock sync.Mutex
	workerSeq  uint64
}

func NewServer(opts ServerOptions) (*Server, error) {
	if opts.WorkerPath == "" {
		return nil, fmt.Errorf("Windows worker path is required")
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 5 * time.Second
	}
	if opts.LogWriter == nil {
		opts.LogWriter = io.Discard
	}
	if opts.Listen.Network == "tcp" && !opts.AllowNonLoopbackListen && !endpoint.IsLoopbackTCP(opts.Listen) {
		return nil, fmt.Errorf("refusing non-loopback listener %s; use the explicit override only in a trusted environment", opts.Listen)
	}
	if opts.Target.Network != "tcp" {
		return nil, fmt.Errorf("Windows relay target must be TCP, got %s", opts.Target)
	}
	if !opts.AllowNonLoopbackTarget && !endpoint.IsLoopbackTCP(opts.Target) {
		return nil, fmt.Errorf("refusing non-loopback Windows target %s", opts.Target)
	}
	if _, err := os.Stat(opts.WorkerPath); err != nil {
		return nil, fmt.Errorf("stat Windows worker %q: %w", opts.WorkerPath, err)
	}

	listener, cleanup, err := listen(opts.Listen)
	if err != nil {
		return nil, err
	}
	return &Server{opts: opts, listener: listener, cleanup: cleanup}, nil
}

func listen(ep endpoint.Endpoint) (net.Listener, func() error, error) {
	cleanup := func() error { return nil }
	if ep.Network == "unix" {
		if err := prepareUnixSocket(ep.Address); err != nil {
			return nil, cleanup, err
		}
		cleanup = func() error {
			info, err := os.Lstat(ep.Address)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSocket == 0 {
				return fmt.Errorf("refusing to remove non-socket path %q", ep.Address)
			}
			return os.Remove(ep.Address)
		}
	}
	ln, err := net.Listen(ep.Network, ep.Address)
	if err != nil {
		_ = cleanup()
		return nil, func() error { return nil }, fmt.Errorf("listen on %s: %w", ep, err)
	}
	if ep.Network == "unix" {
		if err := os.Chmod(ep.Address, 0o600); err != nil {
			_ = ln.Close()
			_ = cleanup()
			return nil, func() error { return nil }, fmt.Errorf("secure Unix socket %q: %w", ep.Address, err)
		}
	}
	return ln, cleanup, nil
}

func prepareUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Unix socket %q: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace non-socket path %q", path)
	}
	conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("Unix socket %q is already active", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove stale Unix socket %q: %w", path, err)
	}
	return nil
}

func (s *Server) Address() string {
	return s.listener.Addr().String()
}

func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	var temporaryDelay time.Duration
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				s.workers.Wait()
				return nil
			}
			if temporary, ok := err.(net.Error); ok && temporary.Temporary() {
				if temporaryDelay == 0 {
					temporaryDelay = 5 * time.Millisecond
				} else {
					temporaryDelay *= 2
				}
				if max := time.Second; temporaryDelay > max {
					temporaryDelay = max
				}
				time.Sleep(temporaryDelay)
				continue
			}
			return fmt.Errorf("accept bridge connection: %w", err)
		}
		temporaryDelay = 0
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			defer conn.Close()
			if err := s.serveConnection(ctx, conn); err != nil && ctx.Err() == nil {
				_, _ = fmt.Fprintf(s.opts.LogWriter, "npu-bridge: connection failed: %v\n", err)
			}
		}()
	}
}

func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.listener.Close()
		if err := s.cleanup(); closeErr == nil {
			closeErr = err
		}
	})
	return closeErr
}

func (s *Server) serveConnection(parent context.Context, client net.Conn) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		s.opts.WorkerPath,
		"relay",
		"--target", s.opts.Target.String(),
		"--connect-timeout", s.opts.ConnectTimeout.String(),
	)
	workerIn, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create worker stdin: %w", err)
	}
	workerOut, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create worker stdout: %w", err)
	}
	cmd.Stderr = s.opts.LogWriter
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Windows worker: %w", err)
	}

	type copyResult struct {
		direction string
		err       error
	}
	copyDone := make(chan copyResult, 2)
	go func() {
		_, copyErr := io.Copy(workerIn, client)
		_ = workerIn.Close()
		copyDone <- copyResult{direction: "request", err: copyErr}
	}()
	go func() {
		_, copyErr := io.Copy(client, workerOut)
		if tcp, ok := client.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		copyDone <- copyResult{direction: "response", err: copyErr}
	}()

	first := <-copyDone
	if first.direction == "response" {
		_ = client.Close()
		_ = workerIn.Close()
	}
	second := <-copyDone
	waitErr := cmd.Wait()

	if first.err != nil && !isClosedConnectionError(first.err) {
		return fmt.Errorf("copy %s stream: %w", first.direction, first.err)
	}
	if second.err != nil && !isClosedConnectionError(second.err) {
		return fmt.Errorf("copy %s stream: %w", second.direction, second.err)
	}
	if waitErr != nil && parent.Err() == nil {
		return fmt.Errorf("Windows worker exited: %w", waitErr)
	}
	return nil
}

func isClosedConnectionError(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed)
}
