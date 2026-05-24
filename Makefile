# Variables may be overridden, e.g. `make build VERSION=v0.0.3`.

VERSION    ?= $(shell git describe --tags --always --dirty --match='v[0-9]*.[0-9]*.[0-9]*' --exclude='*-dev.*' 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

GO ?= go

.PHONY: help build build-all test vet fmt-check fmt lint vuln ci clean

help: ## show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?##/ { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## build host-arch binary into bin/sct-agent
	$(GO) build -ldflags '$(LDFLAGS)' -o bin/sct-agent ./cmd/agent

build-all: ## cross-compile bin/sct-agent-linux-{amd64,arm64}
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags '$(LDFLAGS)' -o bin/sct-agent-linux-amd64 ./cmd/agent
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags '$(LDFLAGS)' -o bin/sct-agent-linux-arm64 ./cmd/agent

test: ## go test with race detector (requires cgo)
	CGO_ENABLED=1 $(GO) test -race ./...

vet: ## go vet
	$(GO) vet ./...

fmt-check: ## fail if any file is not gofmt-clean
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt issues:"; echo "$$out"; exit 1; fi

fmt: ## apply gofmt fixes
	gofmt -w .

lint: ## run golangci-lint (resolved via go.mod tool directive)
	$(GO) tool golangci-lint run

vuln: ## run govulncheck (resolved via go.mod tool directive)
	$(GO) tool govulncheck ./...

ci: fmt-check vet lint vuln test build-all ## full CI suite

clean: ## remove build artifacts
	rm -rf bin/
