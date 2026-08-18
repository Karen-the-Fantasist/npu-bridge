// npu-bridge-test-backend is a Windows-loopback HTTP fixture for cross-boundary
// tests. It is not part of the production bridge binaries.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	listenAddress := flag.String("listen", "127.0.0.1:0", "Windows loopback listen address")
	maxRequests := flag.Int64("max-requests", 2, "exit after this many completed HTTP requests")
	maxLifetime := flag.Duration("max-lifetime", 15*time.Second, "hard upper bound for the fixture lifetime")
	flag.Parse()

	host, _, err := net.SplitHostPort(*listenAddress)
	if err != nil {
		fatalf("invalid --listen: %v", err)
	}
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		fatalf("test backend refuses non-loopback listener %q", *listenAddress)
	}
	if *maxRequests <= 0 {
		fatalf("--max-requests must be positive")
	}

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		fatalf("listen: %v", err)
	}
	defer listener.Close()

	var completed atomic.Int64
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.Copy(w, r.Body)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unavailable", http.StatusInternalServerError)
			return
		}
		for i := 0; i < 3; i++ {
			payload, _ := json.Marshal(map[string]int{"index": i})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
		}
	})
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r)
		if completed.Add(1) >= *maxRequests {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
			}()
		}
	})

	_, _ = fmt.Fprintf(os.Stdout, "LISTEN tcp://%s\n", listener.Addr())
	_ = os.Stdout.Sync()
	lifetime := time.AfterFunc(*maxLifetime, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	defer lifetime.Stop()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fatalf("serve: %v", err)
	}
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "npu-bridge-test-backend: "+format+"\n", args...)
	os.Exit(1)
}
