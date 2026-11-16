BINARY  := ytm
PKG     := github.com/Franciss-Prog/go-ytm
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/internal/buildinfo.Version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.Commit=$(COMMIT) \
	-X $(PKG)/internal/buildinfo.Date=$(DATE)

GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null)

.PHONY: all build test lint run bench fmt tidy clean check-go

all: lint test build

check-go:
	@test -f go.mod || { echo "no go.mod yet (Phase 0: specs only)"; exit 0; }

build: check-go
	@test ! -f go.mod || CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/ytm

test: check-go
	@test ! -f go.mod || go test -race -count=1 ./...

lint: check-go
	@test ! -f go.mod || go vet ./...
ifneq ($(GOLANGCI_LINT),)
	@test ! -f go.mod || $(GOLANGCI_LINT) run ./...
else
	@test ! -f go.mod || go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run ./...
endif

run: build
	@test ! -f go.mod || ./bin/$(BINARY) $(ARGS)

bench: check-go
	@test ! -f go.mod || go test -run '^$$' -bench . -benchmem ./...

fmt:
	@test ! -f go.mod || gofmt -s -w .

tidy:
	@test ! -f go.mod || go mod tidy

clean:
	rm -rf bin/ dist/
