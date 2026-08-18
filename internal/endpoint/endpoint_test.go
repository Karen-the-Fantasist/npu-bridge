package endpoint

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		network string
		address string
	}{
		{"127.0.0.1:8080", "tcp", "127.0.0.1:8080"},
		{"tcp://localhost:8000", "tcp", "localhost:8000"},
		{"tcp://[::1]:9000", "tcp", "[::1]:9000"},
		{"unix:///tmp/npu-bridge.sock", "unix", "/tmp/npu-bridge.sock"},
		{"/tmp/npu-bridge.sock", "unix", "/tmp/npu-bridge.sock"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got.Network != tt.network || got.Address != tt.address {
				t.Fatalf("Parse() = %#v, want network=%q address=%q", got, tt.network, tt.address)
			}
		})
	}
}

func TestParseRejectsInvalidEndpoints(t *testing.T) {
	for _, in := range []string{"", "localhost", "http://localhost:80", "tcp://localhost:80/path", "unix://relative/path"} {
		t.Run(in, func(t *testing.T) {
			if _, err := Parse(in); err == nil {
				t.Fatalf("Parse(%q) unexpectedly succeeded", in)
			}
		})
	}
}

func TestIsLoopbackTCP(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{"127.0.0.1:80", true},
		{"127.4.3.2:80", true},
		{"[::1]:80", true},
		{"localhost:80", true},
		{"0.0.0.0:80", false},
		{"192.168.1.2:80", false},
		{"example.com:80", false},
	}
	for _, tt := range tests {
		e := Endpoint{Network: "tcp", Address: tt.address}
		if got := IsLoopbackTCP(e); got != tt.want {
			t.Fatalf("IsLoopbackTCP(%q) = %v, want %v", tt.address, got, tt.want)
		}
	}
}
