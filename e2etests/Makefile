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

.PHONY: build test lint coverage verify test-local test-profile-gating clean

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

test-local: test-builder-local test-profile-gating

.PHONY: test-builder-local
test-builder-local:
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
	printf '%s\n' '{"applianceProfile":"builder","allowedGitSourceHosts":["git.internal.example.com"],"allowedBuilderImageDigests":["buildah@sha256:approved"],"buildCatalog":{"workProfiles":[{"name":"builder","description":"Builder workflows"}],"repos":[{"name":"app","url":"https://git.internal.example.com/team/app","defaultRef":"0123456789abcdef0123456789abcdef01234567"}],"buildTargets":[{"name":"default","aliases":["app"],"workProfile":"builder","repo":"app","execution":"repo_script","imageRepository":"users/alice/app","imageTagTemplate":"{commit12}","builderImageDigest":"buildah@sha256:approved"}]}}' >"$$server_config_file"; \
	$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-stop TEST_RUN_DIR="$$server_run_dir_abs" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" >/dev/null 2>&1 || true; \
	$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-bootstrap-init TEST_RUN_DIR="$$server_run_dir_abs" TEST_CONFIG_FILE="$$server_config_file" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" TEST_ADMIN_USERNAME="$(TEST_ADMIN_USERNAME)" TEST_PASSWORD_FILE="$$admin_password_file"; \
	$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-start TEST_RUN_DIR="$$server_run_dir_abs" TEST_CONFIG_FILE="$$server_config_file" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)"; \
	{ test_status=0; \
		$(GO) run ./local/integration-test \
			--api-base-url "$(TEST_API_BASE_URL)" \
			--server-binary "$(TEST_SERVER_BINARY)" \
			--data-dir "$$server_data_dir_abs" \
			--config-file "$$server_config_file" \
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

test-profile-gating:
	@set -e; \
	mkdir -p "$(RUN_DIR)" "$(LOG_DIR)"; \
	run_dir_abs="$$(cd "$(RUN_DIR)" && pwd)"; \
	admin_password_file="$$run_dir_abs/profile-gating-admin-password.txt"; \
	printf '%s\n' "$(TEST_ADMIN_PASSWORD)" >"$$admin_password_file"; \
	for profile in core storage; do \
		server_run_dir_abs="$$run_dir_abs/profile-gating-$$profile"; \
		server_config_file="$$server_run_dir_abs/config.json"; \
		rm -rf "$$server_run_dir_abs"; \
		mkdir -p "$$server_run_dir_abs"; \
		printf '%s\n' "{\"applianceProfile\":\"$$profile\"}" >"$$server_config_file"; \
		$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-stop TEST_RUN_DIR="$$server_run_dir_abs" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" >/dev/null 2>&1 || true; \
		$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-bootstrap-init TEST_RUN_DIR="$$server_run_dir_abs" TEST_CONFIG_FILE="$$server_config_file" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)" TEST_ADMIN_USERNAME="$(TEST_ADMIN_USERNAME)" TEST_PASSWORD_FILE="$$admin_password_file"; \
		$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-start TEST_RUN_DIR="$$server_run_dir_abs" TEST_CONFIG_FILE="$$server_config_file" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)"; \
		test_status=0; \
		$(GO) run ./local/profile-gating \
			--api-base-url "$(TEST_API_BASE_URL)" \
			--profile "$$profile" \
			--admin-username "$(TEST_ADMIN_USERNAME)" \
			--admin-password "$(TEST_ADMIN_PASSWORD)" || test_status=$$?; \
		$(MAKE) -C "$(BACKEND_DIR)" --no-print-directory test-stop TEST_RUN_DIR="$$server_run_dir_abs" TEST_PUBLIC_ADDR="$(TEST_PUBLIC_ADDR)" TEST_INTERNAL_ADDR="$(TEST_INTERNAL_ADDR)"; \
		if [ $$test_status -ne 0 ]; then \
			exit $$test_status; \
		fi; \
	done

clean:
	rm -rf "$(RUN_DIR)" "$(COVER_FILE)" integration-test
