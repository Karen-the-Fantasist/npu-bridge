# Architecture

## Scope

`npu-bridge` is a byte-stream transport between a WSL-local endpoint and a
Windows-loopback TCP endpoint. It does not define or inspect the application
protocol carried by the stream.

## Components

### Linux listener

The `serve` command runs in WSL. It accepts connections from a loopback TCP
listener or a Unix socket. Each accepted connection starts one Windows relay
worker through WSL interop.

### Windows relay worker

The `relay` command runs as a Windows process. It connects to the configured
Windows TCP endpoint and copies data between that socket and its standard
input/output streams.

### Target service

The target is an existing Windows TCP service. The service owns its protocol,
authentication, lifecycle, resource selection, and error semantics.

## Connection lifecycle

1. The Linux listener accepts a client connection.
2. The listener starts the Windows worker with `relay --target ENDPOINT`.
3. The worker connects to the Windows endpoint.
4. Two copy loops forward the full-duplex stream.
5. End-of-file on either side closes the corresponding stream direction.
6. Listener cancellation closes active client connections and worker
   processes.

## Protocol handling

The bridge adds no application-level framing. The following stream patterns
are supported without special handling:

- persistent TCP connections;
- HTTP/1.1 keep-alive;
- chunked transfer encoding;
- server-sent events;
- binary request and response bodies;
- service-specific paths, headers, and payloads.

## Process model

Each client connection has one worker process. A worker failure affects only
its associated connection. The listener continues accepting other clients.

## Diagnostics

The `probe` command checks TCP connectivity from the process that executes it.
The `doctor` command starts the Windows worker and reports whether that worker
can reach the configured Windows endpoint.

Diagnostic success confirms transport reachability only. It does not validate
the target service's application behavior.

## Test fixture

`cmd/npu-bridge-test-backend` builds a finite Windows loopback HTTP service. It
provides a binary echo endpoint and a three-event server-sent-event endpoint.
The fixture stops after a configured request count or lifetime and is excluded
from the production build target.
