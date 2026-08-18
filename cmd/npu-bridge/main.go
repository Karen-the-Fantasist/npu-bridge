package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"example.com/npu-bridge/internal/doctor"
	"example.com/npu-bridge/internal/endpoint"
	"example.com/npu-bridge/internal/transport"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "npu-bridge: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		writeUsage(stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:], stdout, stderr)
	case "relay":
		return runRelay(args[1:], os.Stdin, stdout, stderr)
	case "probe":
		return runProbe(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "version", "--version", "-version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "help", "--help", "-h":
		writeUsage(stdout)
		return nil
	default:
		writeUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServe(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	listenRaw := fs.String("listen", envOr("NPU_BRIDGE_LISTEN", "tcp://127.0.0.1:11435"), "WSL-side tcp:// or unix:// listener")
	targetRaw := fs.String("target", os.Getenv("NPU_BRIDGE_TARGET"), "required Windows-side loopback TCP service")
	workerPath := fs.String("worker", os.Getenv("NPU_BRIDGE_WORKER"), "path to npu-bridge.exe visible from WSL")
	connectTimeout := fs.Duration("connect-timeout", 5*time.Second, "Windows target connection timeout")
	allowListen := fs.Bool("allow-non-loopback-listen", false, "allow exposing the WSL listener beyond loopback")
	allowTarget := fs.Bool("allow-non-loopback-target", false, "allow the Windows relay to dial beyond loopback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *targetRaw == "" {
		return fmt.Errorf("--target or NPU_BRIDGE_TARGET is required")
	}
	listenEndpoint, err := endpoint.Parse(*listenRaw)
	if err != nil {
		return fmt.Errorf("parse listener: %w", err)
	}
	targetEndpoint, err := endpoint.Parse(*targetRaw)
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}
	if *workerPath == "" {
		*workerPath, err = defaultWorkerPath()
		if err != nil {
			return err
		}
	}

	server, err := transport.NewServer(transport.ServerOptions{
		Listen:                 listenEndpoint,
		Target:                 targetEndpoint,
		WorkerPath:             *workerPath,
		ConnectTimeout:         *connectTimeout,
		AllowNonLoopbackListen: *allowListen,
		AllowNonLoopbackTarget: *allowTarget,
		LogWriter:              stderr,
	})
	if err != nil {
		return err
	}
	defer server.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_, _ = fmt.Fprintf(stdout, "npu-bridge listening on %s -> Windows %s\n", server.Address(), targetEndpoint)
	return server.Serve(ctx)
}

func runRelay(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targetRaw := fs.String("target", "", "target tcp:// endpoint")
	connectTimeout := fs.Duration("connect-timeout", 5*time.Second, "target connection timeout")
	allowTarget := fs.Bool("allow-non-loopback-target", false, "allow dialing beyond loopback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *targetRaw == "" {
		return fmt.Errorf("--target is required")
	}
	target, err := endpoint.Parse(*targetRaw)
	if err != nil {
		return err
	}
	if target.Network != "tcp" {
		return fmt.Errorf("relay target must be TCP")
	}
	if !*allowTarget && !endpoint.IsLoopbackTCP(target) {
		return fmt.Errorf("refusing non-loopback target %s", target)
	}
	return transport.Relay(context.Background(), stdin, stdout, transport.RelayOptions{
		Target:         target,
		ConnectTimeout: *connectTimeout,
	})
}

func runProbe(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targetRaw := fs.String("target", "", "target tcp:// endpoint")
	connectTimeout := fs.Duration("connect-timeout", 5*time.Second, "target connection timeout")
	allowTarget := fs.Bool("allow-non-loopback-target", false, "allow dialing beyond loopback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *targetRaw == "" {
		return fmt.Errorf("--target is required")
	}
	target, err := endpoint.Parse(*targetRaw)
	if err != nil {
		return err
	}
	if target.Network != "tcp" {
		return fmt.Errorf("probe target must be TCP")
	}
	if !*allowTarget && !endpoint.IsLoopbackTCP(target) {
		return fmt.Errorf("refusing non-loopback target %s", target)
	}
	result := transport.Probe(context.Background(), target, *connectTimeout)
	if err := transport.WriteProbeJSON(stdout, result); err != nil {
		return err
	}
	if !result.OK {
		return fmt.Errorf("target probe failed")
	}
	return nil
}

func runDoctor(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targetRaw := fs.String("target", os.Getenv("NPU_BRIDGE_TARGET"), "required Windows-side loopback TCP service")
	workerPath := fs.String("worker", os.Getenv("NPU_BRIDGE_WORKER"), "path to npu-bridge.exe visible from WSL")
	connectTimeout := fs.Duration("connect-timeout", 5*time.Second, "Windows target connection timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *targetRaw == "" {
		return fmt.Errorf("--target or NPU_BRIDGE_TARGET is required")
	}
	target, err := endpoint.Parse(*targetRaw)
	if err != nil {
		return err
	}
	if !endpoint.IsLoopbackTCP(target) {
		return fmt.Errorf("doctor only probes a Windows loopback target")
	}
	if *workerPath == "" {
		*workerPath, err = defaultWorkerPath()
		if err != nil {
			return err
		}
	}
	report := doctor.Run(context.Background(), *workerPath, target, *connectTimeout)
	if err := doctor.WriteJSON(stdout, report); err != nil {
		return err
	}
	if !report.OK {
		return fmt.Errorf("doctor found one or more failures")
	}
	return nil
}

func defaultWorkerPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate current executable: %w", err)
	}
	for _, name := range []string{"npu-bridge.exe", "npu-bridge-windows-amd64.exe"} {
		candidate := filepath.Join(filepath.Dir(executable), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Windows worker not found next to the Linux binary; pass --worker or NPU_BRIDGE_WORKER")
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writeUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, `npu-bridge %s

Usage:
  npu-bridge serve  [--listen tcp://127.0.0.1:11435] --target tcp://127.0.0.1:PORT [--worker /path/to/npu-bridge.exe]
  npu-bridge doctor --target tcp://127.0.0.1:PORT [--worker /path/to/npu-bridge.exe]
  npu-bridge relay  --target tcp://127.0.0.1:PORT
  npu-bridge probe  --target tcp://127.0.0.1:PORT
  npu-bridge version

serve runs inside WSL. relay and probe are normally executed by the Windows
worker. Both sides refuse non-loopback TCP endpoints unless explicitly opted in.
`, version)
}
