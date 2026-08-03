BACKEND_DIR := services/controlplane
UI_DIR      := services/controlplane-ui
HOST_AGENT_SERVICE_DIR := services/hostagent
SDK_DIR     := sdk/golang/applianceclient
CHART_DIR   := deploy/charts/appliance-control-plane
REGISTRY_CHART_DIR := deploy/charts/appliance-registry
DNS_CHART_DIR := deploy/charts/appliance-dns
E2E_DIR     := e2etests
VERIFY_LOG_DIR := $(CURDIR)/.run/logs
VERIFY_BUILD_LOG := $(VERIFY_LOG_DIR)/verify-build.log
VERIFY_LINT_LOG := $(VERIFY_LOG_DIR)/verify-lint.log
VERIFY_TEST_LOG := $(VERIFY_LOG_DIR)/verify-test.log
VERIFY_CURL_LOG := $(VERIFY_LOG_DIR)/verify-curl.log
VERIFY_E2E_LOG := $(VERIFY_LOG_DIR)/verify-e2e.log
VERIFY_COVERAGE_LOG := $(VERIFY_LOG_DIR)/verify-coverage.log
VERIFY_K3S_LOG := $(VERIFY_LOG_DIR)/verify-k3s.log

GO_MODULE_DIRS := $(BACKEND_DIR) $(UI_DIR) $(HOST_AGENT_SERVICE_DIR) $(SDK_DIR) $(CHART_DIR) $(REGISTRY_CHART_DIR) $(DNS_CHART_DIR) $(E2E_DIR)
# Product/release version for packaged images and /version. Prefer an explicit
# CODE_VERSION/PRODUCT_VERSION/IMAGE_TAG from the release flow; otherwise use a
# reachable git tag, not a bare commit SHA from `git describe --always`.
CONTROL_PLANE_CODE_VERSION := $(shell \
	if [ -n "$${IMAGE_TAG:-}" ]; then printf '%s' "$${IMAGE_TAG}"; \
	elif [ -n "$${CODE_VERSION:-}" ]; then printf '%s' "$${CODE_VERSION}"; \
	elif [ -n "$${PRODUCT_VERSION:-}" ]; then printf '%s' "$${PRODUCT_VERSION}"; \
	else raw="$$(git -C $(CURDIR) describe --tags --dirty 2>/dev/null || echo 0.0.0-dev)"; printf '%s' "$$raw"; \
	fi | sed 's/[^A-Za-z0-9_.-]/-/g')

# Per-developer overrides (dev-container image/tag, engine, cache paths).
# See dev-container/env.example. Included early so its plain `=`
# assignments win over the `?=` defaults below.
-include dev-container/env

CONTAINER_ENGINE ?= podman
# Registry host (or legacy host/repo path). Prefer host-only + DEV_IMAGE_REPO.
DEV_REGISTRY     ?= ghcr.io/zoncaesaradmin/development-container
DEV_IMAGE_REPO   ?=
DEV_IMAGE_NAME   ?= dev-build
DEV_IMAGE_TAG    ?= latest
ifeq ($(strip $(DEV_IMAGE_REPO)),)
DEV_IMAGE        ?= $(DEV_REGISTRY)/$(DEV_IMAGE_NAME):$(DEV_IMAGE_TAG)
else
DEV_IMAGE        ?= $(DEV_REGISTRY)/$(DEV_IMAGE_REPO)/$(DEV_IMAGE_NAME):$(DEV_IMAGE_TAG)
endif
# Login host for podman login. Override from the release skill
# (build_flow.dev_image_pull; host derived from registry/image_repo/image_name/image_tag)
# so GHCR and LAN registries both work.
DEV_REGISTRY_HOST ?= $(firstword $(subst /, ,$(DEV_REGISTRY)))
# TLS verify for outer podman login/pull of the shared dev-container image
# and for control-plane `make image` push (forwarded into the container).
# Set false for LAN registries with self-signed / host-mismatch certs.
DEV_REGISTRY_TLS_VERIFY ?= true
DEV_REGISTRY_AUTH_FILE ?= $(HOME)/.config/containers/auth.json
DEV_CACHE_DIR    ?= $(HOME)/.cache/appliance-code-dev
DEV_VOLUME_OPTS  ?=
# Explicit credentials for dev container registry login/pulls.
DEV_REGISTRY_USER ?=
DEV_REGISTRY_TOKEN ?=
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
DEV_ENGINE_TLS_FLAGS :=
DEV_ENGINE_PULL_FLAGS :=
ifeq ($(CONTAINER_ENGINE),podman)
DEV_ENGINE_AUTH_FLAGS += --authfile "$(DEV_REGISTRY_AUTH_FILE)"
DEV_ENGINE_TLS_FLAGS += --tls-verify=$(DEV_REGISTRY_TLS_VERIFY)
# Refresh :latest (and other mutable tags) when the registry has a newer
# digest; without this, make dev-shell reuses a stale local image forever.
DEV_ENGINE_PULL_FLAGS += --pull=newer
endif
DEV_FORWARD_ENV_VARS := DEV_REGISTRY_USER DEV_REGISTRY_TOKEN DEV_IMAGE_TAG DEV_IMAGE_NAME DEV_REGISTRY DEV_IMAGE_REPO DEV_REGISTRY_TLS_VERIFY SERVICE_IMAGE_REGISTRY SERVICE_IMAGE_REPO SERVICE_IMAGE_NAME SERVICE_IMAGE_TAG
DEV_FORWARD_ENV_FLAGS := $(foreach var,$(DEV_FORWARD_ENV_VARS),-e $(var))
SUDOERS_FILE := /etc/sudoers.d/appliance-podman-nopasswd

.PHONY: build test test-curl test-e2e lint coverage verify run stop dev-k3s clean dev-shell dev-run dev-registry-login dev-registry-auth-check dev-sudo-setup package-control-plane-image-archive package-ui-image-archive package-host-agent-image-archive package-argo-controller-image-archive package-zot-image-archive package-coredns-image-archive package-host-packages package-release-input-tar

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
	@$(MAKE) -C $(REGISTRY_CHART_DIR) lint
	@$(MAKE) -C $(REGISTRY_CHART_DIR) template
	@$(MAKE) -C $(DNS_CHART_DIR) lint
	@$(MAKE) -C $(DNS_CHART_DIR) template

## clean: remove build/run/coverage artifacts from every module
clean:
	@for module in $(GO_MODULE_DIRS); do \
		$(MAKE) -C "$$module" clean; \
	done
## package-control-plane-image-archive: always build and export the control-plane
## image from this checkout as an OCI archive tarball for release-input packaging.
## Pass IMAGE_TAG/CODE_VERSION/PRODUCT_VERSION for the operator-facing product
## version baked into /version (default: tagged release or 0.0.0-dev).
package-control-plane-image-archive:
	@image_tag="$${IMAGE_TAG:-$${CODE_VERSION:-$${PRODUCT_VERSION:-$(CONTROL_PLANE_CODE_VERSION)}}}"; \
	out_file="$${OUT_FILE:-$(CURDIR)/.run/control-plane-api-$${image_tag}.tar}"; \
	mkdir -p "$$(dirname "$$out_file")"; \
	bash ./scripts/package/export-control-plane-image-archive.sh \
		--out-file "$$out_file" \
		--image-tag "$$image_tag"

## package-ui-image-archive: always build and export the UI service image from
## this checkout as an OCI archive tarball for release-input packaging.
package-ui-image-archive:
	@image_tag="$${IMAGE_TAG:-$${CODE_VERSION:-$${PRODUCT_VERSION:-$(CONTROL_PLANE_CODE_VERSION)}}}"; \
	out_file="$${OUT_FILE:-$(CURDIR)/.run/appliance-ui-$${image_tag}.tar}"; \
	mkdir -p "$$(dirname "$$out_file")"; \
	bash ./scripts/package/export-ui-image-archive.sh \
		--out-file "$$out_file" \
		--image-tag "$$image_tag"

## package-host-agent-image-archive: always build and export the appliance
## host-agent service image as an OCI archive tarball for release-input
## packaging.
package-host-agent-image-archive:
	@image_tag="$${IMAGE_TAG:-$${CODE_VERSION:-$${PRODUCT_VERSION:-$(CONTROL_PLANE_CODE_VERSION)}}}"; \
	out_file="$${OUT_FILE:-$(CURDIR)/.run/appliance-host-agent-$${image_tag}.tar}"; \
	reference_out_file="$${REFERENCE_OUT_FILE:-$${out_file%.tar}.reference}"; \
	mkdir -p "$$(dirname "$$out_file")" "$$(dirname "$$reference_out_file")"; \
	bash ./scripts/package/export-host-agent-image-archive.sh \
		--out-file "$$out_file" \
		--reference-out-file "$$reference_out_file" \
		--image-tag "$$image_tag"

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

## package-zot-image-archive: export the pinned upstream Zot image using the
## canonical bundled annotation and platform-manifest digest reference.
package-zot-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/zot.tar}"; \
	bash ./scripts/package/export-zot-image-archive.sh \
		--out-file "$$out_file" \
		$${ZOT_SOURCE_IMAGE:+--source-image "$${ZOT_SOURCE_IMAGE}"} \
		$${ZOT_VERSION:+--zot-version "$${ZOT_VERSION}"}

## package-coredns-image-archive: build the appliance CoreDNS wrapper
## (upstream binary + log tee entrypoint) and export it with the canonical
## bundled annotation and platform-manifest digest reference.
package-coredns-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/coredns.tar}"; \
	reference_file="$${REFERENCE_OUT_FILE:-$${out_file%.tar}.reference}"; \
	bash ./scripts/package/export-coredns-image-archive.sh \
		--out-file "$$out_file" \
		--reference-out-file "$$reference_file" \
		$${DNS_SOURCE_IMAGE:+--source-image "$${DNS_SOURCE_IMAGE}"} \
		$${DNS_VERSION:+--dns-version "$${DNS_VERSION}"}

## package-host-packages: export the offline Ubuntu host package payload
## needed by installer-owned host capabilities such as mDNS.
package-host-packages:
	@out_dir="$${OUT_DIR:-$(CURDIR)/.run/host-packages}"; \
	mkdir -p "$$(dirname "$$out_dir")"; \
	bash ./scripts/package/export-host-packages.sh \
		--out-dir "$$out_dir" \
		$${OS_VERSION:+--os-version "$${OS_VERSION}"} \
		$${ARCH:+--arch "$${ARCH}"}

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
	host_agent_image="$(CURDIR)/.run/appliance-host-agent-$(CONTROL_PLANE_CODE_VERSION).tar"; \
	host_agent_reference_file="$(CURDIR)/.run/appliance-host-agent-$(CONTROL_PLANE_CODE_VERSION).reference"; \
	host_agent_binary="$(CURDIR)/services/hostagent/bin/appliance-host-agentd"; \
	host_mdns_enabled="$${HOST_MDNS_ENABLED:-false}"; \
	host_packages_dir="$${HOST_PACKAGES_DIR:-$(CURDIR)/.run/host-packages}"; \
	host_packages_os_version="$${HOST_PACKAGES_OS_VERSION:-$${OS_VERSION:-24.04}}"; \
	argo_version="$${ARGO_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/argo-workflows/Chart.yaml)}"; \
	argo_controller_image="$(CURDIR)/.run/argo-controller-$$argo_version.tar"; \
	zot_version="$${ZOT_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/appliance-registry/Chart.yaml)}"; \
	zot_image="$${ZOT_IMAGE:-$(CURDIR)/.run/zot-$$zot_version.tar}"; \
	zot_reference_file="$(CURDIR)/.run/zot-$$zot_version.reference"; \
	dns_version="$${DNS_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/appliance-dns/Chart.yaml)}"; \
	dns_image="$${DNS_IMAGE:-$(CURDIR)/.run/coredns-$$dns_version.tar}"; \
	dns_reference_file="$(CURDIR)/.run/coredns-$$dns_version.reference"; \
	control_plane_image_ref="localhost/appliance-control-plane:$(CONTROL_PLANE_CODE_VERSION)"; \
	ui_image_ref="localhost/appliance-ui:$(CONTROL_PLANE_CODE_VERSION)"; \
	argo_controller_image_ref="localhost/appliance-argo-controller:$$argo_version"; \
	$(MAKE) --no-print-directory package-control-plane-image-archive OUT_FILE="$$control_plane_image"; \
	$(MAKE) --no-print-directory package-ui-image-archive OUT_FILE="$$ui_image"; \
	$(MAKE) --no-print-directory -C ./services/hostagent build; \
	$(MAKE) --no-print-directory package-host-agent-image-archive \
		OUT_FILE="$$host_agent_image" \
		REFERENCE_OUT_FILE="$$host_agent_reference_file"; \
	if [ "$$host_mdns_enabled" = "true" ] && [ -z "$${HOST_PACKAGES_DIR:-}" ]; then \
		$(MAKE) --no-print-directory package-host-packages \
			OUT_DIR="$$host_packages_dir" \
			OS_VERSION="$$host_packages_os_version"; \
	fi; \
	host_agent_image_ref="$$(tr -d '\r\n' < "$$host_agent_reference_file")"; \
	if [ -n "$$argo_version" ] && [ -z "$${ARGO_CONTROLLER_IMAGE:-}" ]; then \
		$(MAKE) --no-print-directory package-argo-controller-image-archive \
			OUT_FILE="$$argo_controller_image" \
			ARGO_VERSION="$$argo_version" \
			ARGO_CONTROLLER_BASE_IMAGE="$${ARGO_CONTROLLER_BASE_IMAGE:-quay.io/argoproj/workflow-controller:$$argo_version}"; \
		ARGO_CONTROLLER_IMAGE="$$argo_controller_image"; \
		ARGO_CONTROLLER_IMAGE_REFERENCE="$${ARGO_CONTROLLER_IMAGE_REFERENCE:-$$argo_controller_image_ref}"; \
	fi; \
	if [ -z "$${ZOT_IMAGE:-}" ]; then \
		bash ./scripts/package/export-zot-image-archive.sh \
			--out-file "$$zot_image" \
			--reference-out-file "$$zot_reference_file" \
			--zot-version "$$zot_version" \
			$${ZOT_SOURCE_IMAGE:+--source-image "$${ZOT_SOURCE_IMAGE}"}; \
		ZOT_IMAGE_REFERENCE="$$(tr -d '\r\n' < "$$zot_reference_file")"; \
	fi; \
	if [ -z "$${DNS_IMAGE:-}" ]; then \
		bash ./scripts/package/export-coredns-image-archive.sh \
			--out-file "$$dns_image" \
			--reference-out-file "$$dns_reference_file" \
			--dns-version "$$dns_version" \
			$${DNS_SOURCE_IMAGE:+--source-image "$${DNS_SOURCE_IMAGE}"}; \
		DNS_IMAGE_REFERENCE="$$(tr -d '\r\n' < "$$dns_reference_file")"; \
	fi; \
	set -- \
		--out-file "$${OUT_FILE}" \
		--code-version "$${CODE_VERSION:-$(CONTROL_PLANE_CODE_VERSION)}" \
		--control-plane-image "$$control_plane_image" \
		--control-plane-image-reference "$$control_plane_image_ref" \
		--ui-image "$$ui_image" \
		--ui-image-reference "$$ui_image_ref" \
		--host-agent-image "$$host_agent_image" \
		--host-agent-image-reference "$$host_agent_image_ref" \
		--host-agent-binary "$$host_agent_binary" \
		--host-mdns-enabled "$$host_mdns_enabled" \
		--zot-image "$$zot_image" \
		--zot-image-reference "$${ZOT_IMAGE_REFERENCE}" \
		--zot-version "$$zot_version" \
		--dns-image "$$dns_image" \
		--dns-image-reference "$${DNS_IMAGE_REFERENCE}" \
		--dns-version "$$dns_version" \
		--k3s-version "$${K3S_VERSION}"; \
	if [ "$$host_mdns_enabled" = "true" ]; then \
		set -- "$$@" --host-packages-dir "$$host_packages_dir" --host-packages-os-version "$$host_packages_os_version"; \
	fi; \
	if [ -n "$${LATEST_OUT_FILE:-}" ]; then set -- "$$@" --latest-out-file "$${LATEST_OUT_FILE}"; fi; \
	if [ -n "$${RELEASE_ID:-}" ]; then set -- "$$@" --release-id "$${RELEASE_ID}"; fi; \
	if [ -n "$${CHART_VERSION:-}" ]; then set -- "$$@" --chart-version "$${CHART_VERSION}"; fi; \
	if [ -n "$${SUPPORTED_UPGRADE_SOURCE:-}" ]; then set -- "$$@" --supported-upgrade-source "$${SUPPORTED_UPGRADE_SOURCE}"; fi; \
	if [ -n "$${SBOM_DIR:-}" ]; then set -- "$$@" --sbom-dir "$${SBOM_DIR}"; fi; \
	if [ -n "$${PROVENANCE_DIR:-}" ]; then set -- "$$@" --provenance-dir "$${PROVENANCE_DIR}"; fi; \
	if [ -n "$${NOTICES_DIR:-}" ]; then set -- "$$@" --notices-dir "$${NOTICES_DIR}"; fi; \
	if [ -n "$${TESTS_DIR:-}" ]; then set -- "$$@" --tests-dir "$${TESTS_DIR}"; fi; \
	if [ -n "$$argo_version" ]; then set -- "$$@" --argo-version "$$argo_version"; fi; \
	if [ -n "$${ARGO_CONTROLLER_IMAGE:-}" ]; then set -- "$$@" --argo-controller-image "$${ARGO_CONTROLLER_IMAGE}"; fi; \
	if [ -n "$${ARGO_CONTROLLER_IMAGE_REFERENCE:-}" ]; then set -- "$$@" --argo-controller-image-reference "$${ARGO_CONTROLLER_IMAGE_REFERENCE}"; fi; \
	if [ -n "$${ARGO_EXECUTOR_IMAGE:-}" ]; then set -- "$$@" --argo-executor-image "$${ARGO_EXECUTOR_IMAGE}"; fi; \
	if [ -n "$${ARGO_EXECUTOR_IMAGE_REFERENCE:-}" ]; then set -- "$$@" --argo-executor-image-reference "$${ARGO_EXECUTOR_IMAGE_REFERENCE}"; fi; \
	if [ -n "$${ARGO_CRDS_DIR:-}" ]; then set -- "$$@" --argo-crds-dir "$${ARGO_CRDS_DIR}"; fi; \
	bash ./scripts/package/archive-release-input.sh "$$@"

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
# dev-build image with this repo mounted. `make dev-run SCRIPT=...`
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
# match. The dev-container image runs as a non-root user (e.g. "devcontainer")
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
# name is passed to the container command, not to the engine.
# $(SUDO) (empty by default) goes first so rootful Podman is used when set.
# `-e VAR` with no value forwards VAR from the current shell's
# environment (if set) rather than baking a value into the command
# line, so `make -C services/controlplane image`/`push` inside the container
# see the same DEV_* publish values already exported
# on the host — no need to re-export them again inside dev-shell.
#
# --entrypoint "" ignores any image ENTRYPOINT. That matters if a service
# image (with mkdir /data/zon/logs/… entrypoint) was accidentally pushed
# over the development-container tag — otherwise `make dev-shell` dies
# before bash runs. Always pass an explicit command after $(DEV_IMAGE).
#
# Go caches mount into the image non-root home (not /root/…): the
# development-container image runs as USERNAME=devcontainer.
DEV_CONTAINER_HOME ?= /home/devcontainer
# Forward host Git SSH credentials into the container when available.
# Prefer the running ssh-agent socket; also mount ~/.ssh read-only so
# key-file auth works without an agent. Without this, `git pull` inside
# `make dev-shell` fails with Permission denied (publickey) even though
# the same command works on the host.
DEV_SSH_FLAGS :=
ifneq ($(SSH_AUTH_SOCK),)
ifeq ($(shell test -S "$(SSH_AUTH_SOCK)" && echo yes),yes)
DEV_SSH_FLAGS += -e SSH_AUTH_SOCK=/ssh-agent -v "$(SSH_AUTH_SOCK):/ssh-agent"
endif
endif
ifeq ($(shell test -d "$(HOME)/.ssh" && echo yes),yes)
DEV_SSH_FLAGS += -v "$(HOME)/.ssh:$(DEV_CONTAINER_HOME)/.ssh:ro"
endif
DEV_RUN = $(SUDO) $(CONTAINER_ENGINE) run --rm --privileged --device /dev/fuse \
	--entrypoint "" \
	$(DEV_ENGINE_AUTH_FLAGS) \
	$(DEV_ENGINE_TLS_FLAGS) \
	$(DEV_ENGINE_PULL_FLAGS) \
	$(DEV_FORWARD_ENV_FLAGS) \
	$(DEV_SSH_FLAGS) \
	-v "$(CURDIR):/workspace$(DEV_VOLUME_OPTS)" \
	-v "$(DEV_CACHE_DIR)/go-build:$(DEV_CONTAINER_HOME)/.cache/go-build$(DEV_VOLUME_OPTS)" \
	-v "$(DEV_CACHE_DIR)/go-mod:$(DEV_CONTAINER_HOME)/go/pkg/mod$(DEV_VOLUME_OPTS)" \
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
##      only DEV_REGISTRY_USER/DEV_REGISTRY_TOKEN/DEV_IMAGE_TAG/DEV_IMAGE_NAME/
##      DEV_REGISTRY/DEV_IMAGE_REPO/DEV_REGISTRY_TLS_VERIFY plus SERVICE_IMAGE_REGISTRY/
##      SERVICE_IMAGE_REPO/SERVICE_IMAGE_NAME/SERVICE_IMAGE_TAG through sudo (so `-e VAR`
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
	if [ -z "$(DEV_REGISTRY_USER)" ] || [ -z "$(DEV_REGISTRY_TOKEN)" ]; then \
		echo "dev-registry-login: DEV_REGISTRY_USER and DEV_REGISTRY_TOKEN must both be set (never interactive):" >&2; \
		echo "  export DEV_REGISTRY_USER=<registry-username>" >&2; \
		echo "  export DEV_REGISTRY_TOKEN=<registry-token-or-PAT>" >&2; \
		echo "  # login host: DEV_REGISTRY_HOST=$(DEV_REGISTRY_HOST) (override for LAN registries)" >&2; \
		exit 1; \
	fi; \
	login_tls_flag="--tls-verify=true"; \
	case "$(DEV_REGISTRY_TLS_VERIFY)" in \
		0|false|FALSE|no|NO|off|OFF) login_tls_flag="--tls-verify=false" ;; \
	esac; \
	mkdir -p "$$(dirname "$(DEV_REGISTRY_AUTH_FILE)")"; \
	chmod 700 "$$(dirname "$(DEV_REGISTRY_AUTH_FILE)")"; \
	printf '%s\n' "$(DEV_REGISTRY_TOKEN)" | podman login $$login_tls_flag --authfile "$(DEV_REGISTRY_AUTH_FILE)" --username "$(DEV_REGISTRY_USER)" --password-stdin $(DEV_REGISTRY_HOST)

dev-registry-auth-check:
	@if [ "$(CONTAINER_ENGINE)" != "podman" ]; then exit 0; fi; \
	if [ -f "$(DEV_REGISTRY_AUTH_FILE)" ]; then exit 0; fi; \
	echo "dev-registry-auth-check: missing Podman auth file: $(DEV_REGISTRY_AUTH_FILE)" >&2; \
	echo "dev-registry-auth-check: create it once non-interactively with:" >&2; \
	echo "  export DEV_REGISTRY_USER=<github-username>" >&2; \
	echo "  export DEV_REGISTRY_TOKEN=<PAT with read:packages>" >&2; \
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
		&& [ "$$(DEV_REGISTRY_USER=$$probe_user sudo -n env 2>/dev/null | sed -n 's/^DEV_REGISTRY_USER=//p')" = "$$probe_user" ] \
		&& [ "$$(DEV_IMAGE_TAG=$$probe_tag sudo -n env 2>/dev/null | sed -n 's/^DEV_IMAGE_TAG=//p')" = "$$probe_tag" ]; then \
		: already configured; \
	else \
		echo "dev-sudo-setup: one-time setup — configuring passwordless sudo + env passthrough for $$podman_path (you may be prompted for your password once)"; \
		{ \
			echo "$$(whoami) ALL=(root) NOPASSWD: $$podman_path"; \
			echo "Defaults:$$(whoami) env_keep += \"DEV_REGISTRY_USER DEV_REGISTRY_TOKEN DEV_IMAGE_TAG DEV_IMAGE_NAME DEV_REGISTRY DEV_IMAGE_REPO DEV_REGISTRY_TLS_VERIFY SERVICE_IMAGE_REGISTRY SERVICE_IMAGE_REPO SERVICE_IMAGE_NAME SERVICE_IMAGE_TAG\""; \
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
