BINARY := ecompeg2ts
PKG := ./cmd/ecompeg2ts

.PHONY: all build test clean linux-arm64 linux-amd64

all: test build

build:
	go build -o bin/$(BINARY) $(PKG)

test:
	go test ./...

linux-arm64:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(BINARY)-linux-arm64 $(PKG)

linux-amd64:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/$(BINARY)-linux-amd64 $(PKG)

clean:
	rm -rf bin dist
