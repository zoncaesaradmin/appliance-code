SHELL := /bin/bash

MODULE      := appliance-code/services/hostagent
BIN_DIR     := bin
# Cluster pod binary (image builds this via Containerfile; local target: build-agent).
AGENT_BINARY := $(BIN_DIR)/appliance-host-agent
# Host OS daemon for lan-discovery / day-2 host apply (packaging: build-agentd).
AGENTD_BINARY := $(BIN_DIR)/appliance-host-agentd
COVER_FILE  := coverage.out

SERVICE_IMAGE_NAME ?= appliance-host-agent
SERVICE_IMAGE_TAG ?= $(shell git -C $(CURDIR) describe --tags --dirty 2>/dev/null || echo 0.0.0-dev)
RUNTIME_PROFILE ?= runtime
SERVICE_IMAGE_BUILD_ARGS = \
	--build-arg RUNTIME_PROFILE=$(RUNTIME_PROFILE)
ifneq ($(strip $(GO_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg GO_IMAGE=$(GO_IMAGE)
endif
ifneq ($(strip $(RUNTIME_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg RUNTIME_IMAGE=$(RUNTIME_IMAGE)
endif
ifneq ($(strip $(RUNTIME_PREBAKED)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg RUNTIME_PREBAKED=$(RUNTIME_PREBAKED)
endif

include $(CURDIR)/../../build/service-image.mk

.PHONY: build build-agentd build-agent test lint coverage clean

# Packaging ownership:
#   build-agentd — foundation lan-discovery (host binary → release-input hostAgentBinary)
#   build-agent  — local/dev only; in-cluster image builds via Containerfile
#   build        — both (local verify / day-to-day)
build-agentd:
	mkdir -p $(BIN_DIR)
	go build -o $(AGENTD_BINARY) ./cmd/appliance-host-agentd

build-agent:
	mkdir -p $(BIN_DIR)
	go build -o $(AGENT_BINARY) ./cmd/appliance-host-agent

build: build-agentd build-agent

test:
	go test ./...

lint:
	go vet ./...
	@fmt_files="$$(gofmt -l .)"; \
	if [ -n "$$fmt_files" ]; then \
		echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; \
	fi

coverage:
	go test -count=1 -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

clean:
	rm -rf $(BIN_DIR) $(COVER_FILE)
