BINARY := ecompeg2ts
PKG := ./cmd/ecompeg2ts

.PHONY: all build test clean linux-arm64 linux-amd64 linux-arm64-tc ebpf-object

all: test build

build:
	go build -o bin/$(BINARY) $(PKG)

test:
	go test ./...

linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(BINARY)-linux-arm64 $(PKG)

linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(BINARY)-linux-amd64 $(PKG)

linux-arm64-tc:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(BINARY)-tc-linux-arm64 ./cmd/ecompeg2ts-tc

ebpf-object:
	mkdir -p dist
	clang -O2 -g -target bpf -I/usr/include/$$(uname -m)-linux-gnu -c bpf/ecompeg2ts_tc.c -o dist/ecompeg2ts_tc_bpfel.o

clean:
	rm -rf bin dist
