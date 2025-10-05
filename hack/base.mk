GOLANGCI_VERSION ?= v2.5.0
PROTOC_VERSION ?= 32.1
TOOLS_BIN := $(shell mkdir -p build/tools && realpath build/tools)

ifneq ($(shell git status --porcelain --untracked-files=no),)
	DIRTY = -dirty
endif
VERSION = $(shell git rev-parse --short HEAD)$(DIRTY)

GOLANGCI = $(TOOLS_BIN)/golangci-lint-$(GOLANGCI_VERSION)
$(GOLANGCI):
	rm -f $(TOOLS_BIN)/golangci-lint*
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_VERSION)/install.sh | sh -s -- -b $(TOOLS_BIN) $(GOLANGCI_VERSION)
	mv $(TOOLS_BIN)/golangci-lint $(TOOLS_BIN)/golangci-lint-$(GOLANGCI_VERSION)


PROTOC = $(TOOLS_BIN)/protoc
$(PROTOC):
	curl -fsSL https://github.com/protocolbuffers/protobuf/releases/download/v$(PROTOC_VERSION)/protoc-$(PROTOC_VERSION)-linux-x86_64.zip \
		-o $(TOOLS_BIN)/protoc.zip
	unzip -j $(TOOLS_BIN)/protoc.zip -d $(TOOLS_BIN) "bin/protoc"
	rm $(TOOLS_BIN)/protoc.zip

	$(call go-install-tool,protoc-gen-go,google.golang.org/protobuf/cmd/protoc-gen-go@latest)
	$(call go-install-tool,protoc-gen-go-grpc,google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest)

# go-install-tool will 'go install' any package $2 and install it as $1.
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
echo "Downloading $(2)" ;\
GOBIN=$(TOOLS_BIN) go install $(2) ;\
}
endef

verify-dirty:
ifneq ($(shell git status --porcelain --untracked-files=no),)
	@echo worktree is dirty
	@git --no-pager status
	@git --no-pager diff
	@exit 1
endif
