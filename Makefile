GO ?= go
OUT_DIR ?= dist
VERSION ?= $(shell git describe --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: test build build-test-backend test-cross-boundary clean

test:
	$(GO) test ./...

build:
	mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/npu-bridge-linux-amd64 ./cmd/npu-bridge
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT_DIR)/npu-bridge-windows-amd64.exe ./cmd/npu-bridge

build-test-backend:
	mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w" -o $(OUT_DIR)/npu-bridge-test-backend-windows-amd64.exe ./cmd/npu-bridge-test-backend

test-cross-boundary: build build-test-backend
	./scripts/test-cross-boundary.sh

clean:
	$(GO) clean -testcache
