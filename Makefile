BINARY := bento
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build test lint release clean

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/bento

test:
	go test ./... -race -count=1

test-integration:
	go test ./... -race -count=1 -tags=integration

lint:
	golangci-lint run

release:
	goreleaser release --clean

clean:
	rm -rf bin/ dist/
