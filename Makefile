BINARY = segments
VERSION = $(shell git describe --tags --always --dirty)
LDFLAGS = -ldflags "-X main.version=$(VERSION)"

EXE =
ifeq ($(OS),Windows_NT)
  EXE = .exe
endif

.PHONY: build install stress clean

build:
	cp web/index.html internal/server/index.html
	CGO_ENABLED=1 go build $(LDFLAGS) -o $(BINARY) ./cmd/segments/

install: build
	cp $(BINARY) ~/.local/bin/segments

stress:
	mkdir -p bin && go build -o bin/stress$(EXE) ./cmd/stress

cross-linux-amd64:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=x86_64-linux-musl-gcc go build $(LDFLAGS) -o $(BINARY)-linux-amd64 ./cmd/segments/

cross-linux-arm64:
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-musl-gcc go build $(LDFLAGS) -o $(BINARY)-linux-arm64 ./cmd/segments/

cross-windows-amd64:
	cp web/index.html internal/server/index.html
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc go build -ldflags "-X main.version=$(VERSION) -extldflags '-static'" -o $(BINARY)-windows-amd64.exe ./cmd/segments/

clean:
	rm -f $(BINARY) $(BINARY)-*
