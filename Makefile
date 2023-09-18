
# TODO: move to build/bin
TARGET_BIN ?= ~/.local/bin/qubesome

.PHONY: build
build:
	go build -o $(TARGET_BIN) main.go
