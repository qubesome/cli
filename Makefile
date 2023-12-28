
# TODO: move to build/bin
TARGET_BIN ?= ~/.local/bin/qubesome

.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -ldflags '-extldflags -static -s -w' -o $(TARGET_BIN) main.go

.PHONY: test
test:
	go test -race ./...
