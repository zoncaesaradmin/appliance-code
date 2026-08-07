SHELL := /bin/bash

MODULE      := appliance-code/services/controlplane-ui
BIN_DIR     := bin
BINARY      := $(BIN_DIR)/appliance-ui
UI_DIST_DIR := $(BIN_DIR)/dist
RUN_DIR     := .run
PID_FILE    := $(RUN_DIR)/appliance-ui.pid
LOG_FILE    := $(RUN_DIR)/appliance-ui.log
COVER_FILE  := coverage.out
NPM         ?= npm

VERSION     ?= $(shell git -C $(CURDIR) describe --tags --dirty 2>/dev/null || echo 0.0.0-dev)
COMMIT      ?= $(shell git -C $(CURDIR) rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X '$(MODULE)/internal/version.Version=$(VERSION)' \
           -X '$(MODULE)/internal/version.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/version.BuildTime=$(BUILD_TIME)'

# Service image: SERVICE_IMAGE_NAME from Makefile / env / make CLI.
# Registry/repo defaults: build/service-image.mk (DEV_REGISTRY + appliance-images).
SERVICE_IMAGE_NAME ?= appliance-ui
SERVICE_IMAGE_BUILD_ARGS = \
	--build-arg VERSION=$(VERSION) \
	--build-arg COMMIT=$(COMMIT) \
	--build-arg BUILD_TIME=$(BUILD_TIME)
ifneq ($(strip $(UI_NODE_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg UI_NODE_IMAGE=$(UI_NODE_IMAGE)
endif
ifneq ($(strip $(UI_GO_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg UI_GO_IMAGE=$(UI_GO_IMAGE)
endif
ifneq ($(strip $(UI_RUNTIME_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg UI_RUNTIME_IMAGE=$(UI_RUNTIME_IMAGE)
endif
ifneq ($(strip $(UI_WEB_DEPS_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg UI_WEB_DEPS_IMAGE=$(UI_WEB_DEPS_IMAGE)
endif
ifneq ($(strip $(USE_PREBAKED_NPM)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg USE_PREBAKED_NPM=$(USE_PREBAKED_NPM)
endif
ifneq ($(strip $(RUNTIME_PREBAKED)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg RUNTIME_PREBAKED=$(RUNTIME_PREBAKED)
endif

include $(CURDIR)/../../build/service-image.mk

.PHONY: build test lint coverage verify run stop clean vendor ui-install ui-dev ui-dev-mock ui-build ui-typecheck ui-test ui-openapi-check ui-preview
# image and image-local come from build/service-image.mk

build: ui-build
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
	$(MAKE) --no-print-directory ui-typecheck

coverage:
	go test -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

verify: build lint ui-openapi-check ui-test test

vendor:
	GOWORK=off go mod tidy
	GOWORK=off go mod vendor

run: build
	mkdir -p $(RUN_DIR)
	@if [ -f $(PID_FILE) ] && kill -0 "$$(cat $(PID_FILE))" 2>/dev/null; then \
		echo "appliance-ui already running (pid $$(cat $(PID_FILE)))"; \
		exit 1; \
	fi
	APPLIANCE_UI_STATIC_DIR="$(CURDIR)/$(UI_DIST_DIR)" nohup ./$(BINARY) > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE)
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

clean: stop
	rm -rf $(BIN_DIR) $(RUN_DIR) $(COVER_FILE)

ui-install:
	$(NPM) ci

ui-dev:
	$(NPM) run dev

ui-dev-mock:
	$(NPM) run dev:mock

ui-build:
	$(NPM) ci
	APPLIANCE_UI_OUT_DIR="$(CURDIR)/$(UI_DIST_DIR)" $(NPM) run build

ui-typecheck:
	$(NPM) ci
	$(NPM) run typecheck

ui-test:
	$(NPM) ci
	$(NPM) run test:ui

ui-openapi-check:
	$(NPM) ci
	$(NPM) run openapi:check

ui-preview:
	APPLIANCE_UI_OUT_DIR="$(CURDIR)/$(UI_DIST_DIR)" $(NPM) run preview
