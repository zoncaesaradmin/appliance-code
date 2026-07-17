SHELL := /bin/bash

MODULE      := appliance-code/services/ui
BIN_DIR     := bin
BINARY      := $(BIN_DIR)/appliance-ui
RUN_DIR     := .run
PID_FILE    := $(RUN_DIR)/appliance-ui.pid
LOG_FILE    := $(RUN_DIR)/appliance-ui.log
COVER_FILE  := coverage.out

VERSION     ?= $(shell git -C $(CURDIR) describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git -C $(CURDIR) rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X '$(MODULE)/internal/version.Version=$(VERSION)' \
           -X '$(MODULE)/internal/version.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/version.BuildTime=$(BUILD_TIME)'

ENGINE_BIN   ?= buildah
BUILD_ENGINE ?= $(ENGINE_BIN) bud
IMAGE_NAME   ?= appliance-ui
IMAGE_TAG    ?= $(VERSION)
IMAGE_REF    := $(IMAGE_NAME):$(IMAGE_TAG)

.PHONY: build test lint coverage verify run stop clean image-local vendor

build:
	mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/appliance-ui

test:
	go test ./...

lint:
	go vet ./...
	@fmt_files="$$(gofmt -l . | grep -v '^vendor/')"; \
	if [ -n "$$fmt_files" ]; then \
		echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; \
	fi

coverage:
	go test -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

verify: lint test

vendor:
	GOWORK=off go mod tidy
	GOWORK=off go mod vendor

run: build
	mkdir -p $(RUN_DIR)
	@if [ -f $(PID_FILE) ] && kill -0 "$$(cat $(PID_FILE))" 2>/dev/null; then \
		echo "appliance-ui already running (pid $$(cat $(PID_FILE)))"; \
		exit 1; \
	fi
	nohup ./$(BINARY) > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE)
	@sleep 1
	@echo "appliance-ui started (pid $$(cat $(PID_FILE))), logs at $(LOG_FILE)"

stop:
	@if [ -f $(PID_FILE) ]; then \
		pid="$$(cat "$(PID_FILE)")"; \
		if kill -0 "$$pid" 2>/dev/null; then \
			kill "$$pid" && echo "stopped appliance-ui (pid $$pid)"; \
		fi; \
		rm -f "$(PID_FILE)"; \
	else \
		echo "no pid file at $(PID_FILE); nothing to stop"; \
	fi

image-local:
	$(BUILD_ENGINE) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-f Containerfile \
		-t $(IMAGE_REF) \
		.

clean: stop
	rm -rf $(BIN_DIR) $(RUN_DIR) $(COVER_FILE)
