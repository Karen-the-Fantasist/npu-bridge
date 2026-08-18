# npu-bridge

`npu-bridge` forwards byte streams from a local WSL endpoint to a TCP service
bound to Windows loopback. The forwarded service may use any application
protocol or hardware backend.

```text
WSL client
    |
    | TCP loopback or Unix socket
    v
Linux npu-bridge listener
    |
    | Windows worker stdin/stdout
    v
Windows npu-bridge.exe worker
    |
    | Windows TCP loopback
    v
Windows service
```

The bridge does not parse application data, manage the target service, or
select its compute device.

## Requirements

- WSL with Windows executable interop enabled.
- Go 1.22 or later for source builds.
- A TCP service listening on Windows loopback.

## Build

```bash
make test
make build
```

The build produces:

- `dist/npu-bridge-linux-amd64`
- `dist/npu-bridge-windows-amd64.exe`

The source has no third-party Go dependencies.

## Run

Set `TARGET_PORT` to the Windows service port:

```bash
TARGET_PORT=8080

./dist/npu-bridge-linux-amd64 doctor \
  --worker ./dist/npu-bridge-windows-amd64.exe \
  --target "tcp://127.0.0.1:${TARGET_PORT}"

./dist/npu-bridge-linux-amd64 serve \
  --listen tcp://127.0.0.1:11435 \
  --worker ./dist/npu-bridge-windows-amd64.exe \
  --target "tcp://127.0.0.1:${TARGET_PORT}"
```

WSL clients connect to `127.0.0.1:11435`. The application protocol and request
paths are defined by the Windows service.

Use a Unix socket for a filesystem-scoped listener:

```bash
./dist/npu-bridge-linux-amd64 serve \
  --listen "unix:///run/user/$(id -u)/npu-bridge.sock" \
  --worker ./dist/npu-bridge-windows-amd64.exe \
  --target "tcp://127.0.0.1:${TARGET_PORT}"
```

The Unix socket is created with mode `0600`.

## Configuration

`serve` accepts:

- `--listen`: WSL-side `tcp://` or `unix://` endpoint. Default:
  `tcp://127.0.0.1:11435`.
- `--target`: required Windows-side `tcp://` endpoint.
- `--worker`: Windows worker path visible from WSL. The command also checks for
  a worker next to the Linux binary.
- `--connect-timeout`: Windows target connection timeout. Default: `5s`.
- `--allow-non-loopback-listen`: permits a non-loopback WSL TCP listener.
- `--allow-non-loopback-target`: permits a non-loopback worker target.

The corresponding environment variables are `NPU_BRIDGE_LISTEN`,
`NPU_BRIDGE_TARGET`, and `NPU_BRIDGE_WORKER`.

## Transport behavior

- One Windows worker is created for each accepted client connection.
- Data is copied in both directions without application-level framing.
- HTTP keep-alive, chunked transfer, server-sent events, and binary payloads
  pass through unchanged.
- Listener cancellation closes active connections and terminates their workers.
- The bridge does not add authentication or modify target credentials.

## Safety defaults

- TCP listeners and targets are restricted to loopback.
- Unix socket paths do not replace regular files or symbolic links.
- No firewall rule, service, driver, scheduled task, or elevation request is
  created.
- Non-loopback access requires an explicit command-line override.

See [architecture](docs/architecture.md) and [security](SECURITY.md) for the
component and trust-boundary specifications.

## Test

```bash
make test
make test-cross-boundary
```

The cross-boundary test uses a finite Windows loopback fixture. It verifies
binary HTTP bodies and server-sent events without requiring an external
backend.

## License

MIT.
