package transport

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"example.com/npu-bridge/internal/endpoint"
)

func TestMinimalEndToEndBridge(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the command binary used as the worker")
	}
	worker := filepath.Join(t.TempDir(), "npu-bridge-worker")
	build := exec.Command("go", "build", "-o", worker, "../../cmd/npu-bridge")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build worker: %v\n%s", err, output)
	}

	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	backendDone := make(chan error, 1)
	go func() {
		conn, acceptErr := backend.Accept()
		if acceptErr != nil {
			backendDone <- acceptErr
			return
		}
		defer conn.Close()
		request, readErr := io.ReadAll(conn)
		if readErr != nil {
			backendDone <- readErr
			return
		}
		_, writeErr := conn.Write(append([]byte("backend:"), request...))
		backendDone <- writeErr
	}()

	server, err := NewServer(ServerOptions{
		Listen:     endpoint.Endpoint{Network: "tcp", Address: "127.0.0.1:0"},
		Target:     endpoint.Endpoint{Network: "tcp", Address: backend.Addr().String()},
		WorkerPath: worker,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx) }()

	client, err := net.DialTimeout("tcp", server.Address(), time.Second)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	payload := []byte{'w', 's', 'l', 0, 0xff}
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	if want := append([]byte("backend:"), payload...); !bytes.Equal(response, want) {
		t.Fatalf("response = %v, want %v", response, want)
	}
	if err := <-backendDone; err != nil {
		t.Fatalf("backend error = %v", err)
	}
	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not stop after cancellation")
	}
}

func TestServerRejectsNonLoopbackByDefault(t *testing.T) {
	worker := makeWorkerFixture(t)
	tests := []ServerOptions{
		{
			Listen:     endpoint.Endpoint{Network: "tcp", Address: "0.0.0.0:0"},
			Target:     endpoint.Endpoint{Network: "tcp", Address: "127.0.0.1:19001"},
			WorkerPath: worker,
		},
		{
			Listen:     endpoint.Endpoint{Network: "tcp", Address: "127.0.0.1:0"},
			Target:     endpoint.Endpoint{Network: "tcp", Address: "192.168.1.2:19001"},
			WorkerPath: worker,
		},
	}
	for i, opts := range tests {
		if server, err := NewServer(opts); err == nil {
			_ = server.Close()
			t.Fatalf("case %d unexpectedly accepted a non-loopback endpoint", i)
		}
	}
}

func TestUnixListenerRefusesToReplaceRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets are tested on the WSL side")
	}
	worker := makeWorkerFixture(t)
	path := filepath.Join(t.TempDir(), "bridge.sock")
	if err := os.WriteFile(path, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewServer(ServerOptions{
		Listen:     endpoint.Endpoint{Network: "unix", Address: path},
		Target:     endpoint.Endpoint{Network: "tcp", Address: "127.0.0.1:19001"},
		WorkerPath: worker,
	})
	if err == nil {
		t.Fatal("NewServer() unexpectedly replaced a regular file")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != "keep me" {
		t.Fatalf("regular file was altered: data=%q err=%v", data, readErr)
	}
}

func makeWorkerFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "npu-bridge.exe")
	if err := os.WriteFile(path, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
