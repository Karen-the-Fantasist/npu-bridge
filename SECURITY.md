# Security model

`npu-bridge` is a local, single-user transport. It is not a network gateway.

## Default boundary

The Linux listener accepts only a loopback TCP address or a Unix socket. The
Windows worker dials only a loopback TCP address. Hostnames other than
`localhost` are rejected by default so DNS rebinding cannot silently turn a
local target into a remote target.

The Unix listener is created with mode `0600`. If a stale filesystem entry is a
regular file or symlink rather than a socket, the bridge refuses to replace it.

## No privilege changes

The bridge does not:

- change Windows Firewall or Hyper-V Firewall rules;
- bind a Windows service to `0.0.0.0`;
- install a system service or scheduled task;
- request UAC elevation;
- install drivers or expose a WSL device node.

## Trust assumptions

Any process that can reach the WSL listener can send requests accepted by the
Windows backend. Prefer a `0600` Unix socket when the backend has no
authentication and the WSL environment is shared.

The child process inherits the launching user's Windows identity. Backend files
and credentials must be protected for that user. Do not pass secrets in
command-line arguments; operating systems commonly expose process arguments.

The explicit `--allow-non-loopback-*` flags broaden the trust boundary. They are
intended for controlled diagnostics, not the default setup.
