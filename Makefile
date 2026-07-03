BACKEND_DIR := server/backend
SDK_DIR     := server/sdk/golang/applianceclient
CHART_DIR   := deploy/charts/appliance-control-plane
E2E_DIR     := e2etests
VERIFY_LOG_DIR := $(CURDIR)/.run/logs
VERIFY_BUILD_LOG := $(VERIFY_LOG_DIR)/verify-build.log
VERIFY_LINT_LOG := $(VERIFY_LOG_DIR)/verify-lint.log
VERIFY_TEST_LOG := $(VERIFY_LOG_DIR)/verify-test.log
VERIFY_CURL_LOG := $(VERIFY_LOG_DIR)/verify-curl.log
VERIFY_E2E_LOG := $(VERIFY_LOG_DIR)/verify-e2e.log
VERIFY_COVERAGE_LOG := $(VERIFY_LOG_DIR)/verify-coverage.log
VERIFY_K3S_LOG := $(VERIFY_LOG_DIR)/verify-k3s.log

GO_MODULE_DIRS := $(BACKEND_DIR) $(SDK_DIR) $(CHART_DIR) $(E2E_DIR)

.PHONY: build test test-curl test-e2e lint coverage verify run stop dev-k3s clean

## build: compile the local server binary (server/backend/bin/appliance-server)
build:
	@set -e; \
	for module in $(GO_MODULE_DIRS); do \
		echo "build stage: $$module"; \
		$(MAKE) -C "$$module" build; \
	done

## test: run unit/integration tests across every module
test:
	@set -e; \
	for module in $(GO_MODULE_DIRS); do \
		echo "test stage: $$module"; \
		$(MAKE) -C "$$module" test; \
	done

## test-curl: run the backend's curl-based live HTTP reference flow
test-curl:
	@$(MAKE) -C $(BACKEND_DIR) test-curl

## test-e2e: run the local live-server SDK-driven end-to-end suite
test-e2e:
	@$(MAKE) -C $(E2E_DIR) test-local

## lint: vet/staticcheck/gofmt across every module
lint:
	@set -e; \
	for module in $(GO_MODULE_DIRS); do \
		echo "lint stage: $$module"; \
		$(MAKE) -C "$$module" lint; \
	done

## coverage: run coverage across every module
coverage:
	@set -e; \
	for module in $(GO_MODULE_DIRS); do \
		echo "coverage stage: $$module"; \
		$(MAKE) -C "$$module" coverage; \
	done

## verify: the repo-wide local pre-push gate; must pass without containers or K3s
verify:
	@set -e; \
	mkdir -p "$(VERIFY_LOG_DIR)"; \
	echo "verify stage: build"; \
	if ! $(MAKE) --no-print-directory build >"$(VERIFY_BUILD_LOG)" 2>&1; then \
		echo "verify: build failed"; \
		echo "verify: inspect $(VERIFY_BUILD_LOG)"; \
		exit 1; \
	fi; \
	echo "verify stage: build passed"; \
	echo "verify stage: lint"; \
	if ! $(MAKE) --no-print-directory lint >"$(VERIFY_LINT_LOG)" 2>&1; then \
		echo "verify: lint failed"; \
		echo "verify: inspect $(VERIFY_LINT_LOG)"; \
		exit 1; \
	fi; \
	echo "verify stage: lint passed"; \
	echo "verify stage: unit/module tests"; \
	if ! $(MAKE) --no-print-directory test >"$(VERIFY_TEST_LOG)" 2>&1; then \
		echo "verify: unit/module tests failed"; \
		echo "verify: inspect $(VERIFY_TEST_LOG)"; \
		exit 1; \
	fi; \
	echo "verify stage: unit/module tests passed"; \
	echo "verify stage: backend curl checks"; \
	if ! $(MAKE) --no-print-directory test-curl >"$(VERIFY_CURL_LOG)" 2>&1; then \
		echo "verify: backend curl checks failed"; \
		echo "verify: inspect $(VERIFY_CURL_LOG)"; \
		exit 1; \
	fi; \
	echo "verify stage: backend curl checks passed"; \
	echo "verify stage: local live-server e2e"; \
	if ! $(MAKE) --no-print-directory test-e2e >"$(VERIFY_E2E_LOG)" 2>&1; then \
		echo "verify: local live-server e2e failed"; \
		echo "verify: inspect $(VERIFY_E2E_LOG)"; \
		exit 1; \
	fi; \
	echo "verify stage: local live-server e2e passed"; \
	echo "verify stage: coverage"; \
	if ! $(MAKE) --no-print-directory coverage >"$(VERIFY_COVERAGE_LOG)" 2>&1; then \
		echo "verify: coverage failed"; \
		echo "verify: inspect $(VERIFY_COVERAGE_LOG)"; \
		exit 1; \
	fi; \
	echo "verify stage: coverage passed"; \
	echo "verify stage: chart render/lint"; \
	if ! $(MAKE) --no-print-directory dev-k3s >"$(VERIFY_K3S_LOG)" 2>&1; then \
		echo "verify: chart render/lint failed"; \
		echo "verify: inspect $(VERIFY_K3S_LOG)"; \
		exit 1; \
	fi; \
	echo "verify stage: chart render/lint passed"; \
	echo "verify stage: clean"; \
	$(MAKE) --no-print-directory clean >/dev/null 2>&1; \
	echo "verify stage: clean passed"; \
	echo "verify stage: passed"

## run: start the control plane locally in the background
run:
	@$(MAKE) -C $(BACKEND_DIR) run

## stop: stop the locally started control plane, if any
stop:
	@$(MAKE) -C $(BACKEND_DIR) stop

## dev-k3s: render and lint the control-plane Helm chart locally (static
## check only; a real K3s host is required for install/restart/air-gap
## evidence, see the Phase 0 note in docs/control-plane-v1-plan.md)
dev-k3s:
	@$(MAKE) -C $(CHART_DIR) lint
	@$(MAKE) -C $(CHART_DIR) template

## clean: remove build/run/coverage artifacts from every module
clean:
	@for module in $(GO_MODULE_DIRS); do \
		$(MAKE) -C "$$module" clean; \
	done
