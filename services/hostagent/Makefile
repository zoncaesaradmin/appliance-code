SHELL := /bin/bash

MODULE      := appliance-code/services/hostagent
BIN_DIR     := bin
SERVICE_BINARY := $(BIN_DIR)/appliance-host-agent
DAEMON_BINARY := $(BIN_DIR)/appliance-host-agentd
COVER_FILE  := coverage.out

SERVICE_IMAGE_NAME ?= appliance-host-agent
SERVICE_IMAGE_TAG ?= $(shell git -C $(CURDIR) describe --tags --dirty 2>/dev/null || echo 0.0.0-dev)
RUNTIME_PROFILE ?= runtime
SERVICE_IMAGE_BUILD_ARGS = \
	--build-arg RUNTIME_PROFILE=$(RUNTIME_PROFILE)

include $(CURDIR)/../../build/service-image.mk

.PHONY: build test lint coverage clean

build:
	mkdir -p $(BIN_DIR)
	go build -o $(SERVICE_BINARY) ./cmd/appliance-host-agent
	go build -o $(DAEMON_BINARY) ./cmd/appliance-host-agentd

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
