SHELL := /bin/bash

BACKEND_DIR ?= ../server/backend
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

.PHONY: build test lint coverage verify test-local clean

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
	$(GO) test -race -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

verify: lint test

test-local:
	@set -e; \
	mkdir -p "$(RUN_DIR)" "$(LOG_DIR)"; \
	run_dir_abs="$$(cd "$(RUN_DIR)" && pwd)"; \
	server_run_dir_abs="$$run_dir_abs/server"; \
	server_data_dir_abs="$$server_run_dir_abs/data"; \
	server_config_file="$$server_run_dir_abs/config.json"; \
	admin_password_file="$$run_dir_abs/admin-password.txt"; \
	rm -rf "$$server_run_dir_abs"; \
	mkdir -p "$$server_run_dir_abs"; \
	printf '%s\n' "$(TEST_ADMIN_PASSWORD)" >"$$admin_password_file"; \
	printf '%s\n' '{"allowedGitSourceHosts":["git.internal.example.com"]}' >"$$server_config_file"; \
	$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-stop TEST_RUN_DIR="$$server_run_dir_abs" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" >/dev/null 2>&1 || true; \
	$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-bootstrap-init TEST_RUN_DIR="$$server_run_dir_abs" TEST_CONFIG_FILE="$$server_config_file" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" TEST_ADMIN_USERNAME="$(TEST_ADMIN_USERNAME)" TEST_PASSWORD_FILE="$$admin_password_file"; \
	$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-start TEST_RUN_DIR="$$server_run_dir_abs" TEST_CONFIG_FILE="$$server_config_file" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)"; \
	{ test_status=0; \
		$(GO) run ./local/integration-test \
			--api-base-url "$(TEST_API_BASE_URL)" \
			--server-binary "$(TEST_SERVER_BINARY)" \
			--data-dir "$$server_data_dir_abs" \
			--public-addr "$(TEST_PUBLIC_ADDR)" \
			--internal-addr "$(TEST_INTERNAL_ADDR)" \
			--admin-username "$(TEST_ADMIN_USERNAME)" \
			--admin-password "$(TEST_ADMIN_PASSWORD)" \
			--alice-password "$(TEST_ALICE_PASSWORD)" \
			--reset-password "$(TEST_RESET_PASSWORD)" \
			--log-file "$(CLIENT_LOG_FILE)" || test_status=$$?; \
		$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-stop TEST_RUN_DIR="$$server_run_dir_abs" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)"; \
		exit $$test_status; \
	}

clean:
	rm -rf "$(RUN_DIR)" "$(COVER_FILE)" integration-test
