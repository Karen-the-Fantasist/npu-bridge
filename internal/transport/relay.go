package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"example.com/npu-bridge/internal/endpoint"
)

type RelayOptions struct {
	Target         endpoint.Endpoint
	ConnectTimeout time.Duration
}

// Relay copies one full-duplex byte stream to a target socket. No HTTP parsing
// happens here, so SSE, chunked responses, WebSockets, and arbitrary binary
// payloads keep their original wire semantics.
func Relay(ctx context.Context, in io.Reader, out io.Writer, opts RelayOptions) error {
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = 5 * time.Second
	}
	dialer := net.Dialer{Timeout: opts.ConnectTimeout, KeepAlive: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, opts.Target.Network, opts.Target.Address)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", opts.Target, err)
	}
	defer conn.Close()

	stopCancellationWatch := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopCancellationWatch:
		}
	}()
	defer close(stopCancellationWatch)

	uploadDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(conn, in)
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		uploadDone <- copyErr
	}()

	_, downloadErr := io.Copy(out, conn)
	_ = conn.Close()
	select {
	case uploadErr := <-uploadDone:
		if downloadErr == nil && uploadErr != nil {
			return fmt.Errorf("copy request stream: %w", uploadErr)
		}
	default:
	}
	if downloadErr != nil {
		return fmt.Errorf("copy response stream: %w", downloadErr)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

type ProbeResult struct {
	OK             bool    `json:"ok"`
	Target         string  `json:"target"`
	ConnectMS      float64 `json:"connect_ms"`
	LocalAddress   string  `json:"local_address,omitempty"`
	RemoteAddress  string  `json:"remote_address,omitempty"`
	Error          string  `json:"error,omitempty"`
	TransportLayer string  `json:"transport_layer"`
}

func Probe(ctx context.Context, target endpoint.Endpoint, timeout time.Duration) ProbeResult {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	started := time.Now()
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, target.Network, target.Address)
	result := ProbeResult{
		Target:         target.String(),
		ConnectMS:      float64(time.Since(started).Microseconds()) / 1000,
		TransportLayer: "windows-loopback",
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.LocalAddress = conn.LocalAddr().String()
	result.RemoteAddress = conn.RemoteAddr().String()
	_ = conn.Close()
	return result
}

func WriteProbeJSON(w io.Writer, result ProbeResult) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(result)
}
