SHELL := /bin/bash

MODULE      := appliance-code/services/hostservice
BIN_DIR     := bin
BROKER_BINARY := $(BIN_DIR)/appliance-host-agent
DAEMON_BINARY := $(BIN_DIR)/appliance-host-agentd
COVER_FILE  := coverage.out

VERSION     ?= $(shell git -C $(CURDIR) describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git -C $(CURDIR) rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X '$(MODULE)/internal/version.Version=$(VERSION)' \
           -X '$(MODULE)/internal/version.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/version.BuildTime=$(BUILD_TIME)'

SERVICE_IMAGE_NAME ?= appliance-host-agent
RUNTIME_PROFILE ?= runtime
SERVICE_IMAGE_BUILD_ARGS = \
	--build-arg VERSION=$(VERSION) \
	--build-arg COMMIT=$(COMMIT) \
	--build-arg BUILD_TIME=$(BUILD_TIME) \
	--build-arg RUNTIME_PROFILE=$(RUNTIME_PROFILE)

include $(CURDIR)/../../build/service-image.mk

.PHONY: build test lint coverage clean

build:
	mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BROKER_BINARY) ./cmd/appliance-host-service
	go build -ldflags "$(LDFLAGS)" -o $(DAEMON_BINARY) ./cmd/appliance-host-agentd

test:
	go test ./...

lint:
	go vet ./...
	@fmt_files="$$(gofmt -l .)"; \
	if [ -n "$$fmt_files" ]; then \
		echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; \
	fi

coverage:
	go test -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

clean:
	rm -rf $(BIN_DIR) $(COVER_FILE)
