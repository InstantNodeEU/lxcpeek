VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)

lxcpeek:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o lxcpeek .

test:
	go test ./...

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/lxcpeek-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/lxcpeek-linux-arm64 .
	cd dist && sha256sum lxcpeek-* > sha256sums.txt

clean:
	rm -rf lxcpeek dist

.PHONY: lxcpeek test dist clean
