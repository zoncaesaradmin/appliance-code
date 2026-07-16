BACKEND_DIR := services/controlplane
UI_DIR      := services/ui
SDK_DIR     := sdk/golang/applianceclient
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

GO_MODULE_DIRS := $(BACKEND_DIR) $(UI_DIR) $(SDK_DIR) $(CHART_DIR) $(E2E_DIR)
CONTROL_PLANE_CODE_VERSION := $(shell raw="$$(git -C $(CURDIR) describe --tags --always --dirty 2>/dev/null || echo dev)"; printf '%s' "$$raw" | sed 's/[^A-Za-z0-9_.-]/-/g')

# Per-developer overrides (dev-container image/tag, engine, cache paths).
# See dev-container/env.example. Included early so its plain `=`
# assignments win over the `?=` defaults below.
-include dev-container/env

CONTAINER_ENGINE ?= podman
DEV_REGISTRY     ?= ghcr.io/zoncaesaradmin/development-container
DEV_IMAGE_NAME   ?= automation-dev
DEV_IMAGE_TAG    ?= latest
DEV_IMAGE        ?= $(DEV_REGISTRY)/$(DEV_IMAGE_NAME):$(DEV_IMAGE_TAG)
DEV_REGISTRY_HOST := $(firstword $(subst /, ,$(DEV_REGISTRY)))
DEV_REGISTRY_AUTH_FILE ?= $(HOME)/.config/containers/auth.json
DEV_CACHE_DIR    ?= $(HOME)/.cache/appliance-code-dev
DEV_VOLUME_OPTS  ?=
# Rootful Podman is required for `make -C services/controlplane image` to work
# from inside dev-shell: a rootless outer container has only one, fully
# consumed user-namespace mapping, so a nested Buildah build inside it
# can't create the additional mapping a real image layer needs (see
# docs/dev-container.md). Defaults to non-interactive sudo so this works
# out of the box on any host with the one-time NOPASSWD sudoers rule +
# a persistent dev-registry authfile already logged in (see
# docs/dev-container.md); "-n" is
# deliberate — it must never prompt in automation, so a missing/wrong
# sudoers rule fails fast and loud instead of hanging. Override to empty
# (`SUDO=`) via dev-container/env on a host that's already rootful, or
# that only ever uses dev-shell/dev-run for plain interactive debugging
# and hasn't set up the sudoers rule.
SUDO ?= sudo -n
# `podman run` accepts `--authfile`, which lets rootful Podman reuse the
# build user's persistent registry credentials without a separate
# `sudo podman login` bootstrap.
DEV_ENGINE_AUTH_FLAGS :=
ifeq ($(CONTAINER_ENGINE),podman)
DEV_ENGINE_AUTH_FLAGS += --authfile "$(DEV_REGISTRY_AUTH_FILE)"
endif
DEV_FORWARD_ENV_VARS := REGISTRY_USER REGISTRY_TOKEN IMAGE_TAG
DEV_FORWARD_ENV_FLAGS := $(foreach var,$(DEV_FORWARD_ENV_VARS),-e $(var))
SUDOERS_FILE := /etc/sudoers.d/appliance-podman-nopasswd

.PHONY: build test test-curl test-e2e lint coverage verify run stop dev-k3s clean dev-shell dev-run dev-registry-login dev-registry-auth-check dev-sudo-setup package-control-plane-image-archive package-ui-image-archive package-argo-controller-image-archive package-release-input-tar

## build: compile the local server binary (services/controlplane/bin/appliance-server)
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
## package-control-plane-image-archive: always build and export the control-plane
## image from this checkout as an OCI archive tarball for release-input packaging.
## The image/archive version always comes from this repo's git describe identity.
package-control-plane-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/control-plane-api-$(CONTROL_PLANE_CODE_VERSION).tar}"; \
	mkdir -p "$$(dirname "$$out_file")"; \
	bash ./scripts/package/export-control-plane-image-archive.sh \
		--out-file "$$out_file"

## package-ui-image-archive: always build and export the UI service image from
## this checkout as an OCI archive tarball for release-input packaging.
package-ui-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/appliance-ui-$(CONTROL_PLANE_CODE_VERSION).tar}"; \
	mkdir -p "$$(dirname "$$out_file")"; \
	bash ./scripts/package/export-ui-image-archive.sh \
		--out-file "$$out_file"

## package-argo-controller-image-archive: always build and export the
## appliance-owned Argo workflow-controller wrapper image as an OCI archive
## tarball for release-input packaging.
package-argo-controller-image-archive:
	@argo_version="$${ARGO_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/argo-workflows/Chart.yaml)}"; \
	out_file="$${OUT_FILE:-$(CURDIR)/.run/argo-controller-$$argo_version.tar}"; \
	mkdir -p "$$(dirname "$$out_file")"; \
	bash ./scripts/package/export-argo-controller-image-archive.sh \
		--out-file "$$out_file" \
		$${ARGO_CONTROLLER_BASE_IMAGE:+--base-image "$${ARGO_CONTROLLER_BASE_IMAGE}"} \
		$${ARGO_VERSION:+--image-tag "$${ARGO_VERSION}"}

## package-release-input-tar: create the versioned release-input tarball handoff
## by always building the control-plane image archive from this checkout.
## ARGO_CRDS_DIR is required: the Argo Workflows chart is always packaged
## (ADR 0011 requires it in the complete v1 appliance), and a bundle
## shipping the chart without its CRDs installs a workflow controller
## that crash-loops forever on startup.
package-release-input-tar:
	@if [ -z "$${OUT_FILE:-}" ] || [ -z "$${K3S_VERSION:-}" ] || [ -z "$${ARGO_CRDS_DIR:-}" ]; then \
		echo "package-release-input-tar: set OUT_FILE, K3S_VERSION, and ARGO_CRDS_DIR" >&2; \
		exit 2; \
	fi
	@control_plane_image="$(CURDIR)/.run/control-plane-api-$(CONTROL_PLANE_CODE_VERSION).tar"; \
	ui_image="$(CURDIR)/.run/appliance-ui-$(CONTROL_PLANE_CODE_VERSION).tar"; \
	argo_version="$${ARGO_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/argo-workflows/Chart.yaml)}"; \
	argo_controller_image="$(CURDIR)/.run/argo-controller-$$argo_version.tar"; \
	control_plane_image_ref="localhost/appliance-control-plane:$(CONTROL_PLANE_CODE_VERSION)"; \
	ui_image_ref="localhost/appliance-ui:$(CONTROL_PLANE_CODE_VERSION)"; \
	argo_controller_image_ref="localhost/appliance-argo-controller:$$argo_version"; \
	$(MAKE) --no-print-directory package-control-plane-image-archive OUT_FILE="$$control_plane_image"; \
	$(MAKE) --no-print-directory package-ui-image-archive OUT_FILE="$$ui_image"; \
	if [ -n "$$argo_version" ] && [ -z "$${ARGO_CONTROLLER_IMAGE:-}" ]; then \
		$(MAKE) --no-print-directory package-argo-controller-image-archive \
			OUT_FILE="$$argo_controller_image" \
			ARGO_VERSION="$$argo_version" \
			ARGO_CONTROLLER_BASE_IMAGE="$${ARGO_CONTROLLER_BASE_IMAGE:-quay.io/argoproj/workflow-controller:$$argo_version}"; \
		ARGO_CONTROLLER_IMAGE="$$argo_controller_image"; \
		ARGO_CONTROLLER_IMAGE_REFERENCE="$${ARGO_CONTROLLER_IMAGE_REFERENCE:-$$argo_controller_image_ref}"; \
	fi; \
	bash ./scripts/package/archive-release-input.sh \
		--out-file "$${OUT_FILE}" \
		$${LATEST_OUT_FILE:+--latest-out-file "$${LATEST_OUT_FILE}"} \
		--code-version "$${CODE_VERSION:-$(CONTROL_PLANE_CODE_VERSION)}" \
		--control-plane-image "$$control_plane_image" \
		--control-plane-image-reference "$$control_plane_image_ref" \
		--ui-image "$$ui_image" \
		--ui-image-reference "$$ui_image_ref" \
		--k3s-version "$${K3S_VERSION}" \
		$${RELEASE_ID:+--release-id "$${RELEASE_ID}"} \
		$${CHART_VERSION:+--chart-version "$${CHART_VERSION}"} \
		$${SUPPORTED_UPGRADE_SOURCE:+--supported-upgrade-source "$${SUPPORTED_UPGRADE_SOURCE}"} \
		$${SBOM_DIR:+--sbom-dir "$${SBOM_DIR}"} \
		$${PROVENANCE_DIR:+--provenance-dir "$${PROVENANCE_DIR}"} \
		$${NOTICES_DIR:+--notices-dir "$${NOTICES_DIR}"} \
		$${TESTS_DIR:+--tests-dir "$${TESTS_DIR}"} \
		$${argo_version:+--argo-version "$${argo_version}"} \
		$${ARGO_CONTROLLER_IMAGE:+--argo-controller-image "$${ARGO_CONTROLLER_IMAGE}"} \
		$${ARGO_CONTROLLER_IMAGE_REFERENCE:+--argo-controller-image-reference "$${ARGO_CONTROLLER_IMAGE_REFERENCE}"} \
		$${ARGO_EXECUTOR_IMAGE:+--argo-executor-image "$${ARGO_EXECUTOR_IMAGE}"} \
		$${ARGO_EXECUTOR_IMAGE_REFERENCE:+--argo-executor-image-reference "$${ARGO_EXECUTOR_IMAGE_REFERENCE}"} \
		$${ARGO_CRDS_DIR:+--argo-crds-dir "$${ARGO_CRDS_DIR}"}

# --- Developer Container (Linux only — see docs/dev-container.md) -----
# A shared toolchain image (Go, Buildah, Skopeo, etc. — see the image's
# own repo). This is where the control-plane's release container image
# actually gets built (`make -C services/controlplane image`, run from inside
# `make dev-shell`) and also where CI build failures get reproduced
# interactively. Requires a Linux host — the build server or a Linux dev
# machine; macOS is not a supported host for this or any container
# tooling in this repo, so there is no `make image` target at the repo
# root, only inside services/controlplane, meant to be invoked from in here.
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
# build` — see services/controlplane/Makefile's `image` target for why.
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
# `-e VAR` with no value forwards VAR from the current shell's
# environment (if set) rather than baking a value into the command
# line, so `make -C services/controlplane image`/`push` inside the container
# see the same REGISTRY_USER/REGISTRY_TOKEN/IMAGE_TAG already exported
# on the host — no need to re-export them again inside dev-shell.
DEV_RUN = $(SUDO) $(CONTAINER_ENGINE) run --rm --privileged --device /dev/fuse \
	$(DEV_ENGINE_AUTH_FLAGS) \
	$(DEV_FORWARD_ENV_FLAGS) \
	-v "$(CURDIR):/workspace$(DEV_VOLUME_OPTS)" \
	-v "$(DEV_CACHE_DIR)/go-build:/root/.cache/go-build$(DEV_VOLUME_OPTS)" \
	-v "$(DEV_CACHE_DIR)/go-mod:/root/go/pkg/mod$(DEV_VOLUME_OPTS)" \
	-w /workspace

## dev-sudo-setup: one-time, idempotent host bootstrap for rootful nested
## Buildah builds — a prerequisite of dev-shell/dev-run, not meant to be
## run directly. Only acts when CONTAINER_ENGINE is podman and SUDO is
## non-empty (the defaults); a no-op otherwise. Two things happen, both
## skipped automatically once already in place (re-detected by an actual
## functional check each run, not just file existence, so a host bootstrapped
## before this env-passthrough rule existed gets upgraded automatically):
##   1. a NOPASSWD sudoers rule scoped to exactly the podman binary path
##      (never a blanket sudo grant), plus an env_keep rule preserving
##      only REGISTRY_USER/REGISTRY_TOKEN/IMAGE_TAG through sudo (so `-e VAR`
##      name-only forwarding on DEV_RUN's rootful podman actually works —
##      sudo's env_reset default would otherwise silently strip them
##      before podman ever saw them). Writing/rewriting this needs one
##      interactive sudo authentication, unavoidably, whenever it changes.
## The dev-container image pull itself now uses Podman's `--authfile`
## support, pointing rootful Podman at the build user's persistent auth
## file, so there is no separate rootful `podman login` bootstrap here.
## After the sudoers rule is in place, no future make dev-shell/dev-run/image
## ever prompts for a sudo password again on this host.
dev-registry-login:
	@if [ "$(CONTAINER_ENGINE)" != "podman" ]; then \
		echo "dev-registry-login: CONTAINER_ENGINE=$(CONTAINER_ENGINE); this helper is for Podman auth files only" >&2; \
		exit 2; \
	fi; \
	if [ -z "$(REGISTRY_USER)" ] || [ -z "$(REGISTRY_TOKEN)" ]; then \
		echo "dev-registry-login: REGISTRY_USER and REGISTRY_TOKEN must both be set (never interactive):" >&2; \
		echo "  export REGISTRY_USER=<github-username>" >&2; \
		echo "  export REGISTRY_TOKEN=<PAT with read:packages>" >&2; \
		exit 1; \
	fi; \
	mkdir -p "$$(dirname "$(DEV_REGISTRY_AUTH_FILE)")"; \
	chmod 700 "$$(dirname "$(DEV_REGISTRY_AUTH_FILE)")"; \
	printf '%s\n' "$(REGISTRY_TOKEN)" | podman login --authfile "$(DEV_REGISTRY_AUTH_FILE)" --username "$(REGISTRY_USER)" --password-stdin $(DEV_REGISTRY_HOST)

dev-registry-auth-check:
	@if [ "$(CONTAINER_ENGINE)" != "podman" ]; then exit 0; fi; \
	if [ -f "$(DEV_REGISTRY_AUTH_FILE)" ]; then exit 0; fi; \
	echo "dev-registry-auth-check: missing Podman auth file: $(DEV_REGISTRY_AUTH_FILE)" >&2; \
	echo "dev-registry-auth-check: create it once non-interactively with:" >&2; \
	echo "  export REGISTRY_USER=<github-username>" >&2; \
	echo "  export REGISTRY_TOKEN=<PAT with read:packages>" >&2; \
	echo "  make dev-registry-login" >&2; \
	echo "dev-registry-auth-check: if you already keep credentials elsewhere, set DEV_REGISTRY_AUTH_FILE to that path." >&2; \
	exit 1

dev-sudo-setup: dev-registry-auth-check
	@if [ "$(CONTAINER_ENGINE)" != "podman" ] || [ -z "$(SUDO)" ]; then exit 0; fi; \
	podman_path="$$(command -v podman)"; \
	if [ -z "$$podman_path" ]; then \
		echo "dev-sudo-setup: podman not found on PATH, skipping rootful bootstrap"; \
		exit 0; \
	fi; \
	probe_user="dev-sudo-setup-user-probe-$$$$"; \
	probe_tag="dev-sudo-setup-tag-probe-$$$$"; \
	if sudo -n "$$podman_path" --version >/dev/null 2>&1 \
		&& [ "$$(REGISTRY_USER=$$probe_user sudo -n env 2>/dev/null | sed -n 's/^REGISTRY_USER=//p')" = "$$probe_user" ] \
		&& [ "$$(IMAGE_TAG=$$probe_tag sudo -n env 2>/dev/null | sed -n 's/^IMAGE_TAG=//p')" = "$$probe_tag" ]; then \
		: already configured; \
	else \
		echo "dev-sudo-setup: one-time setup — configuring passwordless sudo + env passthrough for $$podman_path (you may be prompted for your password once)"; \
		{ \
			echo "$$(whoami) ALL=(root) NOPASSWD: $$podman_path"; \
			echo "Defaults:$$(whoami) env_keep += \"REGISTRY_USER REGISTRY_TOKEN IMAGE_TAG\""; \
		} | sudo tee "$(SUDOERS_FILE)" >/dev/null; \
		sudo chmod 0440 "$(SUDOERS_FILE)"; \
		if ! sudo visudo -c -f "$(SUDOERS_FILE)" >/dev/null 2>&1; then \
			echo "dev-sudo-setup: sudoers validation failed, rolling back"; \
			sudo rm -f "$(SUDOERS_FILE)"; \
			exit 1; \
		fi; \
		echo "dev-sudo-setup: passwordless sudo + env passthrough for podman configured at $(SUDOERS_FILE)"; \
	fi

## dev-shell: interactive shell in the shared dev-container image, this repo mounted at /workspace
dev-shell: dev-sudo-setup
	@mkdir -p "$(DEV_CACHE_DIR)/go-build" "$(DEV_CACHE_DIR)/go-mod"
	$(DEV_RUN) -it $(DEV_IMAGE) bash -c '$(DEV_ENSURE_VIM); exec bash'

## dev-run: run one script (SCRIPT=path) inside the dev container, then exit — the automation counterpart to dev-shell
dev-run: dev-sudo-setup
	@if [ -z "$(SCRIPT)" ]; then \
		echo "dev-run: pass SCRIPT=<path-to-script-under-the-repo>, e.g. make dev-run SCRIPT=scripts/build-and-push.sh" >&2; \
		exit 2; \
	fi
	@mkdir -p "$(DEV_CACHE_DIR)/go-build" "$(DEV_CACHE_DIR)/go-mod"
	$(DEV_RUN) $(DEV_IMAGE) bash "$(SCRIPT)"
