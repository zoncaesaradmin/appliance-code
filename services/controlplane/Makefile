SHELL := /bin/bash

MODULE      := appliance-code/services/controlplane
BIN_DIR     := bin
BINARY      := $(BIN_DIR)/appliance-server
RUN_DIR     := .run
PID_FILE    := $(RUN_DIR)/appliance-server.pid
LOG_FILE    := $(RUN_DIR)/appliance-server.log
COVER_FILE  := coverage.out
TEST_RUN_DIR ?= $(RUN_DIR)/test-server
TEST_PID_FILE := $(TEST_RUN_DIR)/appliance-server.pid
TEST_LOG_DIR := $(TEST_RUN_DIR)/logs
TEST_LOG_FILE := $(TEST_LOG_DIR)/appliance-server.log
TEST_APPLICATION_LOG_FILE := $(TEST_LOG_DIR)/application.log
TEST_DATA_DIR := $(TEST_RUN_DIR)/data
TEST_CONFIG_FILE ?=
TEST_PUBLIC_ADDR ?= 127.0.0.1:18084
TEST_INTERNAL_ADDR ?= 127.0.0.1:18085
TEST_CANONICAL_ORIGIN ?= http://$(TEST_PUBLIC_ADDR)
TEST_PASSWORD_FILE ?=
TEST_ADMIN_USERNAME ?= admin

VERSION     ?= $(shell git -C $(CURDIR) describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git -C $(CURDIR) rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X '$(MODULE)/internal/version.Version=$(VERSION)' \
           -X '$(MODULE)/internal/version.Commit=$(COMMIT)' \
           -X '$(MODULE)/internal/version.BuildTime=$(BUILD_TIME)'

# Release-engineering image build. Run only on the Linux build server/CI,
# inside the shared dev container (see docs/dev-container.md) — there is
# no host-safety guard here because the build server has no other place
# this would run from; macOS is kept out by never installing container
# tooling there, not by a runtime check.
#
# Uses Buildah directly (not `podman build`): inside an already-rootless
# `make dev-shell` container, `podman build` tries to create a second,
# independent nested user namespace (via newuidmap) for its own build
# step, which the kernel refuses (rootless-in-rootless is not supported
# this way). Buildah with chroot isolation (BUILDAH_ISOLATION=chroot,
# already set by the dev-container image) avoids that nested-namespace
# creation entirely — see docs/dev-container.md.
ENGINE_BIN   ?= buildah
BUILD_ENGINE ?= $(ENGINE_BIN) bud
IMAGE_NAME   ?= appliance-control-plane
# Defaults to VERSION (git describe); override explicitly, e.g.
# `make image IMAGE_TAG=v0.1.0`.
IMAGE_TAG    ?= $(VERSION)
IMAGE_REF    := $(IMAGE_NAME):$(IMAGE_TAG)
# Runtime profile for the final container image. `debug` includes common
# operator tools (bash, vim, curl, ping/dns/process tools); `runtime`
# keeps only the lean base runtime packages.
RUNTIME_PROFILE ?= debug

# Push destination for `make push`. Only REGISTRY needs to change to
# retarget a different registry (e.g. a future internal Zot server).
REGISTRY    ?= ghcr.io
IMAGE_OWNER ?= zoncaesaradmin
IMAGE_REPO  ?= appliance-code
REMOTE_IMAGE := $(REGISTRY)/$(IMAGE_OWNER)/$(IMAGE_REPO)/$(IMAGE_NAME)

.PHONY: build test lint coverage verify run stop test-start test-stop test-bootstrap-init test-live test-curl clean image image-local vendor

## build: compile the local server binary into bin/
build:
	mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/appliance-server

## test: run local unit tests with the race detector
test:
	go test -race ./...

## vendor: refresh the vendored dependency tree (needed after bumping any
## require, e.g. github.com/zoncaesaradmin/platformkit); commit the result
vendor:
	GOWORK=off go mod tidy
	GOWORK=off go mod vendor

## test-curl: start the real local server and exercise a curl-based HTTP reference flow
test-curl: build
	@CURL_TEST_QUIET="$(CURL_TEST_QUIET)" bash tests/curl-test/run.sh

## test-bootstrap-init: create the first administrator against the test data dir
test-bootstrap-init: build
	@if [ -z "$(TEST_PASSWORD_FILE)" ]; then \
		echo "TEST_PASSWORD_FILE is required"; \
		exit 1; \
	fi
	@mkdir -p "$(TEST_RUN_DIR)"
	@APPLIANCE_CONFIG_FILE="$(TEST_CONFIG_FILE)" \
	APPLIANCE_DATA_DIR="$(TEST_DATA_DIR)" \
	APPLIANCE_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" \
	APPLIANCE_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" \
	APPLIANCE_CANONICAL_ORIGIN="$(TEST_CANONICAL_ORIGIN)" \
	APPLIANCE_APPLICATION_LOG_PATH="$(TEST_APPLICATION_LOG_FILE)" \
	./$(BINARY) bootstrap init --admin-username "$(TEST_ADMIN_USERNAME)" --admin-password-file "$(TEST_PASSWORD_FILE)"

## test-start: start the real server on dedicated local loopback test ports
test-start: build
	@mkdir -p "$(TEST_RUN_DIR)" "$(TEST_LOG_DIR)"
	@if [ -f "$(TEST_PID_FILE)" ] && kill -0 "$$(cat "$(TEST_PID_FILE)")" 2>/dev/null; then \
		echo "appliance-server test process already running (pid $$(cat "$(TEST_PID_FILE)"))"; \
		exit 1; \
	fi
	@rm -f "$(TEST_PID_FILE)"
	@APPLIANCE_CONFIG_FILE="$(TEST_CONFIG_FILE)" \
	APPLIANCE_DATA_DIR="$(TEST_DATA_DIR)" \
	APPLIANCE_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" \
	APPLIANCE_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" \
	APPLIANCE_CANONICAL_ORIGIN="$(TEST_CANONICAL_ORIGIN)" \
	APPLIANCE_APPLICATION_LOG_PATH="$(TEST_APPLICATION_LOG_FILE)" \
	nohup ./$(BINARY) >"$(TEST_LOG_FILE)" 2>&1 & echo $$! >"$(TEST_PID_FILE)"
	@attempts=0; \
	until curl -fsS "http://$(TEST_INTERNAL_ADDR)/health/ready" >/dev/null 2>&1; do \
		attempts=$$((attempts + 1)); \
		if [ $$attempts -ge 30 ]; then \
			echo "appliance-server test process did not become ready"; \
			test -f "$(TEST_LOG_FILE)" && tail -n 50 "$(TEST_LOG_FILE)" || true; \
			$(MAKE) --no-print-directory test-stop TEST_RUN_DIR="$(TEST_RUN_DIR)" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)"; \
			exit 1; \
		fi; \
		sleep 1; \
	done
	@echo "appliance-server test process started on http://$(TEST_PUBLIC_ADDR)"

## test-stop: stop the local test server process, if running
test-stop:
	@if [ -f "$(TEST_PID_FILE)" ]; then \
		pid="$$(cat "$(TEST_PID_FILE)")"; \
		if kill -0 "$$pid" 2>/dev/null; then \
			kill "$$pid" 2>/dev/null || true; \
			attempts=0; \
			while kill -0 "$$pid" 2>/dev/null; do \
				attempts=$$((attempts + 1)); \
				if [ $$attempts -ge 20 ]; then \
					kill -9 "$$pid" 2>/dev/null || true; \
					break; \
				fi; \
				sleep 1; \
			done; \
		fi; \
		rm -f "$(TEST_PID_FILE)"; \
	fi

## test-live: start the real backend, run the external live-server E2E client, then stop it
test-live:
	@$(MAKE) --no-print-directory -C ../../e2etests test-local BACKEND_DIR="$(CURDIR)" TEST_SERVER_BINARY="$(CURDIR)/$(BINARY)"

## lint: vet plus staticcheck where available
lint:
	go vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then \
		out="$$(staticcheck ./... 2>&1)"; rc=$$?; \
		if [ $$rc -ne 0 ]; then \
			if echo "$$out" | grep -qi "requires newer go version\|requires go >="; then \
				echo "staticcheck skipped: installed binary predates this module's Go version, reinstall to re-enable:"; \
				echo "  go install honnef.co/go/tools/cmd/staticcheck@latest"; \
			else \
				echo "$$out"; \
				exit 1; \
			fi; \
		fi; \
	else \
		echo "staticcheck not found on PATH, skipping (install: go install honnef.co/go/tools/cmd/staticcheck@latest)"; \
	fi
	@fmt_files="$$(gofmt -l . | grep -v '^vendor/')"; \
	if [ -n "$$fmt_files" ]; then \
		echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; \
	fi

## coverage: run tests with coverage and print a per-function summary
coverage:
	go test -race -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

## verify: the local pre-push gate for this module; must pass without containers or K3s
verify: lint test test-curl

## run: start the control plane locally in the background using local config defaults
run: build
	mkdir -p $(RUN_DIR)
	@if [ -f $(PID_FILE) ] && kill -0 "$$(cat $(PID_FILE))" 2>/dev/null; then \
		echo "appliance-server already running (pid $$(cat $(PID_FILE)))"; \
		exit 1; \
	fi
	APPLIANCE_DATA_DIR=$${APPLIANCE_DATA_DIR:-./data} \
	nohup ./$(BINARY) > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE)
	@sleep 1
	@echo "appliance-server started (pid $$(cat $(PID_FILE))), logs at $(LOG_FILE)"

## stop: stop the locally started server, if any
stop:
	@if [ -f $(PID_FILE) ]; then \
		pid="$$(cat $(PID_FILE))"; \
		if kill -0 "$$pid" 2>/dev/null; then \
			kill "$$pid" && echo "stopped appliance-server (pid $$pid)"; \
		else \
			echo "no running process for recorded pid $$pid"; \
		fi; \
		rm -f $(PID_FILE); \
	else \
		echo "no pid file at $(PID_FILE); nothing to stop"; \
	fi

## image-local: build the release-engineering control-plane container image
## into local container storage without pushing it anywhere
image-local:
	$(BUILD_ENGINE) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg RUNTIME_PROFILE=$(RUNTIME_PROFILE) \
		-f Containerfile \
		-t $(IMAGE_REF) \
		.

## image: build the release-engineering control-plane container image
## and push it to REMOTE_IMAGE (REGISTRY/IMAGE_OWNER/IMAGE_REPO/
## IMAGE_NAME) as both IMAGE_TAG and "latest"; run only on the Linux
## build server/CI, inside the shared dev container — see
## docs/dev-container.md. Builds from vendor/, so the build itself needs
## no network access; run `make vendor` first if it's stale. Pushing
## requires REGISTRY_USER/REGISTRY_TOKEN (scoped to write:packages) —
## never interactive, fails fast if either is unset.
image:
	@if [ -z "$(REGISTRY_USER)" ] || [ -z "$(REGISTRY_TOKEN)" ]; then \
		echo "image: REGISTRY_USER and REGISTRY_TOKEN must both be set (never interactive):" >&2; \
		echo "  export REGISTRY_USER=<github-username>" >&2; \
		echo "  export REGISTRY_TOKEN=<PAT with write:packages>" >&2; \
		exit 1; \
	fi
	$(BUILD_ENGINE) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		--build-arg RUNTIME_PROFILE=$(RUNTIME_PROFILE) \
		-f Containerfile \
		-t $(IMAGE_REF) \
		.
	echo "$(REGISTRY_TOKEN)" | $(ENGINE_BIN) login --username "$(REGISTRY_USER)" --password-stdin $(REGISTRY)
	$(ENGINE_BIN) tag $(IMAGE_REF) $(REMOTE_IMAGE):$(IMAGE_TAG)
	$(ENGINE_BIN) push $(REMOTE_IMAGE):$(IMAGE_TAG)
	@echo "image: pushed $(REMOTE_IMAGE):$(IMAGE_TAG)"
	@if [ "$(IMAGE_TAG)" != "latest" ]; then \
		$(ENGINE_BIN) tag $(IMAGE_REF) $(REMOTE_IMAGE):latest; \
		$(ENGINE_BIN) push $(REMOTE_IMAGE):latest; \
		echo "image: pushed $(REMOTE_IMAGE):latest"; \
	fi

## clean: remove local build, run, and coverage artifacts
clean: stop
	@$(MAKE) --no-print-directory test-stop >/dev/null 2>&1 || true
	rm -rf $(BIN_DIR) $(RUN_DIR) $(COVER_FILE)
