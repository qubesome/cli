TARGET_BIN ?= build/bin/qubesome

include hack/base.mk

.PHONY: help
help: ## display Makefile's help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: build
build: ## build qubesome to the path set on TARGET_BIN.
	CGO_ENABLED=0 go build -trimpath -ldflags '-extldflags -static -s -w' -o $(TARGET_BIN) main.go

.PHONY: test
test: ## run golang tests.
	go test -race ./...

validate: validate-lint validate-dirty ## Run validation checks.

validate-lint: $(GOLANGCI)
	$(GOLANGCI) run
