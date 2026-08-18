package transport

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"example.com/npu-bridge/internal/endpoint"
)

func TestRelayPreservesFullDuplexBytes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer conn.Close()
		request, readErr := io.ReadAll(conn)
		if readErr != nil {
			serverDone <- readErr
			return
		}
		_, writeErr := conn.Write(append([]byte("reply:"), request...))
		serverDone <- writeErr
	}()

	payload := []byte{'a', 0, 'b', '\n', 0xff}
	var response bytes.Buffer
	err = Relay(context.Background(), bytes.NewReader(payload), &response, RelayOptions{
		Target: endpoint.Endpoint{Network: "tcp", Address: listener.Addr().String()},
	})
	if err != nil {
		t.Fatalf("Relay() error = %v", err)
	}
	want := append([]byte("reply:"), payload...)
	if !bytes.Equal(response.Bytes(), want) {
		t.Fatalf("response = %v, want %v", response.Bytes(), want)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("server error = %v", err)
	}
}

func TestProbe(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, _ := listener.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()

	result := Probe(context.Background(), endpoint.Endpoint{Network: "tcp", Address: listener.Addr().String()}, time.Second)
	if !result.OK {
		t.Fatalf("Probe() = %#v", result)
	}
	if result.RemoteAddress == "" || result.ConnectMS < 0 {
		t.Fatalf("Probe() returned incomplete result: %#v", result)
	}
}

func TestProbeFailureIsStructured(t *testing.T) {
	result := Probe(context.Background(), endpoint.Endpoint{Network: "tcp", Address: "127.0.0.1:1"}, 100*time.Millisecond)
	if result.OK || result.Error == "" {
		t.Fatalf("Probe() = %#v, want structured failure", result)
	}
	if !strings.Contains(result.Target, "127.0.0.1:1") {
		t.Fatalf("Probe() target = %q", result.Target)
	}
}
