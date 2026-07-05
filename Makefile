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

# Per-developer overrides (dev-container image/tag, engine, cache paths).
# See dev-container/env.example. Included early so its plain `=`
# assignments win over the `?=` defaults below.
-include dev-container/env

CONTAINER_ENGINE ?= podman
DEV_REGISTRY     ?= ghcr.io/zoncaesaradmin/development-container
DEV_IMAGE_NAME   ?= automation-dev
DEV_IMAGE_TAG    ?= latest
DEV_IMAGE        ?= $(DEV_REGISTRY)/$(DEV_IMAGE_NAME):$(DEV_IMAGE_TAG)
DEV_CACHE_DIR    ?= $(HOME)/.cache/appliance-code-dev
DEV_VOLUME_OPTS  ?=
# Rootful Podman is required for `make -C server/backend image` to work
# from inside dev-shell: a rootless outer container has only one, fully
# consumed user-namespace mapping, so a nested Buildah build inside it
# can't create the additional mapping a real image layer needs (see
# docs/dev-container.md). Set SUDO=sudo (or copy dev-container/env.example)
# on hosts where $(CONTAINER_ENGINE) itself runs rootless.
SUDO ?=

.PHONY: build test test-curl test-e2e lint coverage verify run stop dev-k3s clean dev-shell dev-run

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

# --- Developer Container (Linux only — see docs/dev-container.md) -----
# A shared toolchain image (Go, Buildah, Skopeo, etc. — see the image's
# own repo). This is where the control-plane's release container image
# actually gets built (`make -C server/backend image`, run from inside
# `make dev-shell`) and also where CI build failures get reproduced
# interactively. Requires a Linux host — the build server or a Linux dev
# machine; macOS is not a supported host for this or any container
# tooling in this repo, so there is no `make image` target at the repo
# root, only inside server/backend, meant to be invoked from in here.
#
# `make dev-shell` drops you into an interactive shell in the shared
# automation-dev image with this repo mounted. `make dev-run SCRIPT=...`
# is the non-interactive counterpart for automation: it runs one script
# inside the same container and exits.
#
# --privileged and --device /dev/fuse are required for Buildah inside
# this container to build the control-plane image (nested containers;
# see development-container's own shell-dev target for the same
# requirement). The image build itself uses `buildah bud`, not `podman
# build` — see server/backend/Makefile's `image` target for why.
#
# Both are ephemeral (--rm): `exit` inside `make dev-shell` just tears
# the container down, nothing to clean up afterward. See
# docs/dev-container.md and dev-container/env.example for how to point
# this at a different registry/tag/engine.

# Installs vim on first use if the image doesn't already have one; a
# no-op if it does. Tried in package-manager order; harmless if none
# match. The dev-container image runs as a non-root user (e.g. "vscode")
# with passwordless sudo, not as root, so package-manager calls need a
# `sudo` prefix when not already root.
DEV_ENSURE_VIM := command -v vim >/dev/null 2>&1 || { \
	if [ "$$(id -u)" = 0 ]; then AS_ROOT=""; else AS_ROOT="sudo"; fi; \
	if command -v apt-get >/dev/null 2>&1; then $$AS_ROOT apt-get update -qq && $$AS_ROOT apt-get install -y -qq vim; \
	elif command -v apk >/dev/null 2>&1; then $$AS_ROOT apk add --no-cache vim; \
	elif command -v dnf >/dev/null 2>&1; then $$AS_ROOT dnf install -y -q vim; \
	elif command -v yum >/dev/null 2>&1; then $$AS_ROOT yum install -y -q vim; \
	else echo "warning: no supported package manager found; vim not installed" >&2; fi; }

# Every run flag must precede $(DEV_IMAGE) — anything after the image
# name is passed to the container's entrypoint, not to the engine.
# $(SUDO) (empty by default) goes first so rootful Podman is used when set.
DEV_RUN = $(SUDO) $(CONTAINER_ENGINE) run --rm --privileged --device /dev/fuse \
	-v "$(CURDIR):/workspace$(DEV_VOLUME_OPTS)" \
	-v "$(DEV_CACHE_DIR)/go-build:/root/.cache/go-build$(DEV_VOLUME_OPTS)" \
	-v "$(DEV_CACHE_DIR)/go-mod:/root/go/pkg/mod$(DEV_VOLUME_OPTS)" \
	-w /workspace

## dev-shell: interactive shell in the shared dev-container image, this repo mounted at /workspace
dev-shell:
	@mkdir -p "$(DEV_CACHE_DIR)/go-build" "$(DEV_CACHE_DIR)/go-mod"
	$(DEV_RUN) -it $(DEV_IMAGE) bash -c '$(DEV_ENSURE_VIM); exec bash'

## dev-run: run one script (SCRIPT=path) inside the dev container, then exit — the automation counterpart to dev-shell
dev-run:
	@if [ -z "$(SCRIPT)" ]; then \
		echo "dev-run: pass SCRIPT=<path-to-script-under-the-repo>, e.g. make dev-run SCRIPT=scripts/build-and-push.sh" >&2; \
		exit 2; \
	fi
	@mkdir -p "$(DEV_CACHE_DIR)/go-build" "$(DEV_CACHE_DIR)/go-mod"
	$(DEV_RUN) $(DEV_IMAGE) bash "$(SCRIPT)"
