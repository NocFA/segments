BINARY = segments
VERSION = $(shell git describe --tags --always --dirty)
LDFLAGS = -ldflags "-X main.version=$(VERSION)"

.PHONY: build install clean

build:
	cp web/index.html internal/server/index.html
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(BINARY) ./cmd/segments/

install: build
	cp $(BINARY) ~/.local/bin/segments

cross-linux-amd64:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=x86_64-linux-musl-gcc go build $(LDFLAGS) -o $(BINARY)-linux-amd64 ./cmd/segments/

cross-linux-arm64:
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-musl-gcc go build $(LDFLAGS) -o $(BINARY)-linux-arm64 ./cmd/segments/

clean:
	rm -f $(BINARY) $(BINARY)-*
