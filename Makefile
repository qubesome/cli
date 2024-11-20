include hack/base.mk

TARGET_BIN ?= build/bin/qubesome

GO_TAGS = -tags 'netgo,osusergo,static_build'
LDFLAGS = -ldflags '-extldflags -static -s -w -X \
	github.com/qubesome/cli/cmd/cli.version=$(VERSION)'

.PHONY: help
help: ## display Makefile's help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: build
build: ## build qubesome to the path set by TARGET_BIN.
	go build -trimpath $(GO_TAGS) $(LDFLAGS) -o $(TARGET_BIN) cmd/qubesome/main.go

.PHONY: test
test: ## run golang tests.
	go test -race -parallel 10 ./...

verify: verify-lint verify-dirty ## Run verification checks.

verify-lint: $(GOLANGCI)
	$(GOLANGCI) run
