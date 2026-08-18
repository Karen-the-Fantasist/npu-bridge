package endpoint

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// Endpoint is a transport endpoint understood by npu-bridge.
type Endpoint struct {
	Network string
	Address string
}

func (e Endpoint) String() string {
	switch e.Network {
	case "tcp":
		return "tcp://" + e.Address
	case "unix":
		return "unix://" + e.Address
	default:
		return e.Network + "://" + e.Address
	}
}

// Parse accepts tcp://host:port, unix:///path, bare host:port, or an absolute
// Unix socket path.
func Parse(raw string) (Endpoint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoint{}, fmt.Errorf("endpoint is empty")
	}

	if !strings.Contains(raw, "://") {
		if filepath.IsAbs(raw) && runtime.GOOS != "windows" {
			return Endpoint{Network: "unix", Address: filepath.Clean(raw)}, nil
		}
		if _, _, err := net.SplitHostPort(raw); err != nil {
			return Endpoint{}, fmt.Errorf("invalid TCP endpoint %q: %w", raw, err)
		}
		return Endpoint{Network: "tcp", Address: raw}, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, fmt.Errorf("parse endpoint %q: %w", raw, err)
	}
	switch u.Scheme {
	case "tcp":
		if u.Path != "" && u.Path != "/" {
			return Endpoint{}, fmt.Errorf("TCP endpoint %q must not contain a path", raw)
		}
		if _, _, err := net.SplitHostPort(u.Host); err != nil {
			return Endpoint{}, fmt.Errorf("invalid TCP endpoint %q: %w", raw, err)
		}
		return Endpoint{Network: "tcp", Address: u.Host}, nil
	case "unix":
		if u.Host != "" {
			return Endpoint{}, fmt.Errorf("Unix endpoint %q must use unix:///absolute/path", raw)
		}
		if !filepath.IsAbs(u.Path) {
			return Endpoint{}, fmt.Errorf("Unix endpoint %q must be absolute", raw)
		}
		return Endpoint{Network: "unix", Address: filepath.Clean(u.Path)}, nil
	default:
		return Endpoint{}, fmt.Errorf("unsupported endpoint scheme %q", u.Scheme)
	}
}

// IsLoopbackTCP reports whether a TCP endpoint names only a loopback address.
// DNS names other than localhost are deliberately rejected to avoid rebinding.
func IsLoopbackTCP(e Endpoint) bool {
	if e.Network != "tcp" {
		return false
	}
	host, _, err := net.SplitHostPort(e.Address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
