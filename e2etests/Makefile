SHELL := /bin/bash

BACKEND_DIR ?= ../services/controlplane
GO ?= go
RUN_DIR ?= .run/local
LOG_DIR ?= $(RUN_DIR)/logs
CLIENT_LOG_FILE ?= $(LOG_DIR)/local-integration-test-client.log
SERVER_RUN_DIR ?= $(RUN_DIR)/server
SERVER_DATA_DIR ?= $(SERVER_RUN_DIR)/data
TEST_PUBLIC_ADDR ?= 127.0.0.1:18084
TEST_INTERNAL_ADDR ?= 127.0.0.1:18085
TEST_API_BASE_URL ?= http://$(TEST_PUBLIC_ADDR)
TEST_ADMIN_USERNAME ?= admin
TEST_ADMIN_PASSWORD ?= admin-password-123
TEST_ALICE_PASSWORD ?= alice-password-123
TEST_RESET_PASSWORD ?= alice-reset-password-123
TEST_SERVER_BINARY ?= $(BACKEND_DIR)/bin/appliance-server
COVER_FILE := coverage.out

.PHONY: build test lint coverage verify clean

build:
	$(GO) build -o /dev/null ./...

test:
	$(GO) test -race ./...

lint:
	go vet ./...
	@fmt_files="$$(gofmt -l .)"; \
	if [ -n "$$fmt_files" ]; then \
		echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; \
	fi

coverage:
	$(GO) test -race -count=1 -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

verify: lint test

clean:
	rm -rf "$(RUN_DIR)" "$(COVER_FILE)" integration-test
