VERSION := $(shell grep -oE '"[0-9]+\.[0-9]+\.[0-9]+"' internal/proto/proto.go | tr -d '"')
LDFLAGS := -s -w

.PHONY: build test dev dist clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/openv6-linux-amd64 .

test:
	go vet ./...
	go test ./...

dev:
	go run . server -listen :18080 -data ./devdata -dry-run

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/openv6-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/openv6-linux-arm64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o dist/openv6-linux-arm .
	@echo "built v$(VERSION) into dist/"
	@echo "tip: copy dist/openv6-linux-* to <panel-data>/bin/ so nodes of every architecture can install from the panel"

clean:
	rm -rf dist devdata
