BACKEND_DIR := services/controlplane
UI_DIR      := services/controlplane-ui
HOST_AGENT_SERVICE_DIR := services/hostagent
SDK_DIR     := sdk/golang/applianceclient
MESSAGING_SDK_DIR := sdk/golang/messaging
CHART_DIR   := deploy/charts/appliance-control-plane
REGISTRY_CHART_DIR := deploy/charts/appliance-registry
DNS_CHART_DIR := deploy/charts/appliance-dns
INFERENCE_CHART_DIR := deploy/charts/appliance-inference
MESSAGE_BROKER_CHART_DIR := deploy/charts/appliance-message-broker
E2E_DIR     := e2etests
VERIFY_LOG_DIR := $(CURDIR)/.run/logs
VERIFY_BUILD_LOG := $(VERIFY_LOG_DIR)/verify-build.log
VERIFY_LINT_LOG := $(VERIFY_LOG_DIR)/verify-lint.log
VERIFY_TEST_LOG := $(VERIFY_LOG_DIR)/verify-test.log
VERIFY_CURL_LOG := $(VERIFY_LOG_DIR)/verify-curl.log
VERIFY_E2E_LOG := $(VERIFY_LOG_DIR)/verify-e2e.log
VERIFY_COVERAGE_LOG := $(VERIFY_LOG_DIR)/verify-coverage.log
VERIFY_K3S_LOG := $(VERIFY_LOG_DIR)/verify-k3s.log

GO_MODULE_DIRS := $(BACKEND_DIR) $(UI_DIR) $(HOST_AGENT_SERVICE_DIR) $(SDK_DIR) $(MESSAGING_SDK_DIR) $(CHART_DIR) $(REGISTRY_CHART_DIR) $(DNS_CHART_DIR) $(INFERENCE_CHART_DIR) $(E2E_DIR)
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
# Tooling registry: one DEV_* set after mode selection (skill maps ONLINE_* → DEV_*
# when online). Defaults preserve local `make dev-shell` without a skill config.
OFFLINE_BUILD ?= 0

DEV_REGISTRY     ?= ghcr.io
DEV_IMAGE_REPO   ?= zoncaesaradmin/development-container
DEV_IMAGE_NAME   ?= dev-build
DEV_IMAGE_TAG    ?= latest
DEV_REGISTRY_USER ?=
DEV_REGISTRY_TOKEN ?=
DEV_REGISTRY_TLS_VERIFY ?= true
DEV_REGISTRY_HOST ?= $(firstword $(subst /, ,$(DEV_REGISTRY)))

# Explicit DEV_IMAGE= override wins; otherwise build from DEV_* parts.
ifeq ($(strip $(DEV_IMAGE_REPO)),)
DEV_IMAGE        ?= $(DEV_REGISTRY)/$(DEV_IMAGE_NAME):$(DEV_IMAGE_TAG)
else
DEV_IMAGE        ?= $(DEV_REGISTRY)/$(DEV_IMAGE_REPO)/$(DEV_IMAGE_NAME):$(DEV_IMAGE_TAG)
endif

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
DEV_ENGINE_TLS_FLAGS :=
DEV_ENGINE_PULL_FLAGS :=
ifeq ($(CONTAINER_ENGINE),podman)
DEV_ENGINE_AUTH_FLAGS += --authfile "$(DEV_REGISTRY_AUTH_FILE)"
DEV_ENGINE_TLS_FLAGS += --tls-verify=$(DEV_REGISTRY_TLS_VERIFY)
# Refresh :latest (and other mutable tags) when the registry has a newer
# digest; without this, make dev-shell reuses a stale local image forever.
DEV_ENGINE_PULL_FLAGS += --pull=newer
endif
DEV_FORWARD_ENV_VARS := DEV_REGISTRY_USER DEV_REGISTRY_TOKEN DEV_IMAGE_TAG DEV_IMAGE_NAME DEV_REGISTRY DEV_IMAGE_REPO DEV_REGISTRY_TLS_VERIFY SERVICE_IMAGE_REGISTRY SERVICE_IMAGE_REPO SERVICE_IMAGE_NAME SERVICE_IMAGE_TAG OFFLINE_BUILD
DEV_FORWARD_ENV_FLAGS := $(foreach var,$(DEV_FORWARD_ENV_VARS),-e $(var))
SUDOERS_FILE := /etc/sudoers.d/appliance-podman-nopasswd

.PHONY: build test test-curl test-e2e lint coverage verify run stop dev-k3s clean dev-shell dev-run dev-registry-login dev-registry-auth-check dev-sudo-setup package-control-plane-image-archive package-ui-image-archive package-host-agent-image-archive package-workflow-controller-image-archive package-artifact-server-image-archive package-dns-server-image-archive package-inference-runtime-image-archive package-video-runtime-image-archive package-message-broker-image-archive package-host-packages package-metadata-bundle package-release-input-tar

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
	echo "verify stage: metadata-bundle embed check"; \
	if ! bash ./scripts/package/sync-embedded-metadata-bundle.sh --check; then \
		echo "verify: metadata-bundle embed check failed"; \
		echo "verify: run ./scripts/package/sync-embedded-metadata-bundle.sh and commit the embedded snapshot"; \
		exit 1; \
	fi; \
	echo "verify stage: metadata-bundle embed check passed"; \
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
	@$(MAKE) -C $(INFERENCE_CHART_DIR) lint
	@$(MAKE) -C $(INFERENCE_CHART_DIR) template
	@$(MAKE) -C $(MESSAGE_BROKER_CHART_DIR) lint
	@$(MAKE) -C $(MESSAGE_BROKER_CHART_DIR) template

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

## package-workflow-controller-image-archive: always build and export the
## appliance-owned workflow-controller wrapper image as an OCI archive
## tarball for release-input packaging.
package-workflow-controller-image-archive:
	@workflows_version="$${WORKFLOWS_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/appliance-workflows/Chart.yaml)}"; \
	out_file="$${OUT_FILE:-$(CURDIR)/.run/workflow-controller-$$workflows_version.tar}"; \
	mkdir -p "$$(dirname "$$out_file")"; \
	bash ./scripts/package/export-workflow-controller-image-archive.sh \
		--out-file "$$out_file" \
		$${WORKFLOW_CONTROLLER_BASE_IMAGE:+--base-image "$${WORKFLOW_CONTROLLER_BASE_IMAGE}"} \
		$${WORKFLOWS_VERSION:+--image-tag "$${WORKFLOWS_VERSION}"}

## package-artifact-server-image-archive: build the appliance artifact-server
## wrapper (upstream registry binary + thin entrypoint; native application.log
## via chart config) and export it with the canonical bundled annotation and
## platform-manifest digest reference (registry.local/artifact-server install
## contract).
package-artifact-server-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/artifact-server.tar}"; \
	reference_file="$${REFERENCE_OUT_FILE:-$${out_file%.tar}.reference}"; \
	src_image="$${ARTIFACT_SERVER_SOURCE_IMAGE:-}"; \
	version="$${ARTIFACT_SERVER_VERSION:-}"; \
	bash ./scripts/package/export-artifact-server-image-archive.sh \
		--out-file "$$out_file" \
		--reference-out-file "$$reference_file" \
		$${src_image:+--source-image "$$src_image"} \
		$${version:+--version "$$version"}

## package-dns-server-image-archive: build the appliance dns-server wrapper
## (upstream binary + log tee entrypoint) and export it with the canonical
## bundled annotation and platform-manifest digest reference.
package-dns-server-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/dns-server.tar}"; \
	reference_file="$${REFERENCE_OUT_FILE:-$${out_file%.tar}.reference}"; \
	bash ./scripts/package/export-dns-server-image-archive.sh \
		--out-file "$$out_file" \
		--reference-out-file "$$reference_file" \
		$${DNS_SOURCE_IMAGE:+--source-image "$${DNS_SOURCE_IMAGE}"} \
		$${DNS_VERSION:+--dns-version "$${DNS_VERSION}"}

## package-inference-runtime-image-archive: re-export the pinned inference
## runtime with registry.local/inference-runtime:bundled annotation and
## platform-manifest digest reference.
package-inference-runtime-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/inference-runtime.tar}"; \
	reference_file="$${REFERENCE_OUT_FILE:-$${out_file%.tar}.reference}"; \
	bash ./scripts/package/export-inference-runtime-image-archive.sh \
		--out-file "$$out_file" \
		--reference-out-file "$$reference_file" \
		$${INFERENCE_SOURCE_IMAGE:+--source-image "$${INFERENCE_SOURCE_IMAGE}"} \
		$${INFERENCE_VERSION:+--inference-version "$${INFERENCE_VERSION}"}

## package-video-runtime-image-archive: re-export the pinned video
## runtime with registry.local/video-runtime:bundled annotation and
## platform-manifest digest reference.
package-video-runtime-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/video-runtime.tar}"; \
	reference_file="$${REFERENCE_OUT_FILE:-$${out_file%.tar}.reference}"; \
	bash ./scripts/package/export-video-runtime-image-archive.sh \
		--out-file "$$out_file" \
		--reference-out-file "$$reference_file" \
		$${VIDEO_SOURCE_IMAGE:+--source-image "$${VIDEO_SOURCE_IMAGE}"} \
		$${VIDEO_VERSION:+--video-version "$${VIDEO_VERSION}"}

package-message-broker-image-archive:
	@out_file="$${OUT_FILE:-$(CURDIR)/.run/message-broker-image.tar}"; \
	reference_file="$${REFERENCE_OUT_FILE:-$${out_file%.tar}.reference}"; \
	bash ./scripts/package/export-message-broker-image-archive.sh \
		--out-file "$$out_file" --reference-out-file "$$reference_file" \
		$${MESSAGE_BROKER_SOURCE_IMAGE:+--source-image "$${MESSAGE_BROKER_SOURCE_IMAGE}"}

## package-host-packages: export the offline Ubuntu host package payload
## for the complete product super-set (mDNS + wifi-client + wifi-ap).
## Install-time flags only enable services; all capability closures are packaged.
## HOST_CAPABILITIES overrides the default: "mdns wifi-client wifi-ap".
package-host-packages:
	@out_dir="$${OUT_DIR:-$(CURDIR)/.run/host-packages}"; \
	mkdir -p "$$(dirname "$$out_dir")"; \
	caps="$${HOST_CAPABILITIES:-mdns wifi-client wifi-ap}"; \
	cap_args=(); \
	for cap in $$caps; do cap_args+=(--capability "$$cap"); done; \
	bash ./scripts/package/export-host-packages.sh \
		--out-dir "$$out_dir" \
		"$${cap_args[@]}" \
		$${OS_VERSION:+--os-version "$${OS_VERSION}"} \
		$${ARCH:+--arch "$${ARCH}"}

## package-metadata-bundle: generate the base appliance metadata-bundle archive.
package-metadata-bundle:
	@bash ./scripts/package/generate-metadata-bundle.sh \
		$${SOFTWARE_VERSION:+--software-version "$${SOFTWARE_VERSION}"} \
		$${METADATA_REVISION:+--metadata-revision "$${METADATA_REVISION}"} \
		--out-dir "$${OUT_DIR:-$(CURDIR)/.run/metadata-bundle}"

## package-release-input-tar: create the versioned release-input tarball handoff
## by always building the control-plane image archive from this checkout.
## WORKFLOWS_CRDS_DIR is required: the workflows chart is always packaged
## (ADR 0011 requires it in the complete v1 appliance), and a bundle
## shipping the chart without its CRDs installs a workflow controller
## that crash-loops forever on startup.
package-release-input-tar:
	@if [ -z "$${OUT_FILE:-}" ] || [ -z "$${K3S_VERSION:-}" ] || [ -z "$${WORKFLOWS_CRDS_DIR:-}" ]; then \
		echo "package-release-input-tar: set OUT_FILE, K3S_VERSION, and WORKFLOWS_CRDS_DIR" >&2; \
		exit 2; \
	fi
	@control_plane_image="$(CURDIR)/.run/control-plane-api-$(CONTROL_PLANE_CODE_VERSION).tar"; \
	ui_image="$(CURDIR)/.run/appliance-ui-$(CONTROL_PLANE_CODE_VERSION).tar"; \
	host_agent_image="$(CURDIR)/.run/appliance-host-agent-$(CONTROL_PLANE_CODE_VERSION).tar"; \
	host_agent_reference_file="$(CURDIR)/.run/appliance-host-agent-$(CONTROL_PLANE_CODE_VERSION).reference"; \
	host_agent_binary="$(CURDIR)/services/hostagent/bin/appliance-host-agentd"; \
	host_packages_dir="$${HOST_PACKAGES_DIR:-$(CURDIR)/.run/host-packages}"; \
	host_packages_os_version="$${HOST_PACKAGES_OS_VERSION:-$${OS_VERSION:-24.04}}"; \
	workflows_version="$${WORKFLOWS_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/appliance-workflows/Chart.yaml)}"; \
	workflow_controller_image="$(CURDIR)/.run/workflow-controller-$$workflows_version.tar"; \
	artifact_server_version="$${ARTIFACT_SERVER_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/appliance-registry/Chart.yaml)}"; \
	artifact_server_image="$${ARTIFACT_SERVER_IMAGE:-$(CURDIR)/.run/artifact-server-$$artifact_server_version.tar}"; \
	artifact_server_reference_file="$(CURDIR)/.run/artifact-server-$$artifact_server_version.reference"; \
	dns_version="$${DNS_VERSION:-$$(sed -n 's/^appVersion: *\"\\{0,1\\}\\([^\"[:space:]]*\\)\"\\{0,1\\}[[:space:]]*$$/\\1/p' ./deploy/charts/appliance-dns/Chart.yaml)}"; \
	dns_image="$${DNS_IMAGE:-$(CURDIR)/.run/dns-server-$$dns_version.tar}"; \
	dns_reference_file="$(CURDIR)/.run/dns-server-$$dns_version.reference"; \
	control_plane_image_ref="localhost/appliance-control-plane:$(CONTROL_PLANE_CODE_VERSION)"; \
	ui_image_ref="localhost/appliance-ui:$(CONTROL_PLANE_CODE_VERSION)"; \
	workflow_controller_image_ref="localhost/appliance-workflow-controller:$$workflows_version"; \
	$(MAKE) --no-print-directory package-control-plane-image-archive OUT_FILE="$$control_plane_image"; \
	$(MAKE) --no-print-directory package-ui-image-archive OUT_FILE="$$ui_image"; \
	$(MAKE) --no-print-directory -C ./services/hostagent build; \
	$(MAKE) --no-print-directory package-host-agent-image-archive \
		OUT_FILE="$$host_agent_image" \
		REFERENCE_OUT_FILE="$$host_agent_reference_file"; \
	if [ -z "$${HOST_PACKAGES_DIR:-}" ]; then \
		$(MAKE) --no-print-directory package-host-packages \
			OUT_DIR="$$host_packages_dir" \
			OS_VERSION="$$host_packages_os_version" \
			HOST_CAPABILITIES="$${HOST_CAPABILITIES:-mdns wifi-client wifi-ap}"; \
	fi; \
	host_agent_image_ref="$$(tr -d '\r\n' < "$$host_agent_reference_file")"; \
	if [ -n "$$workflows_version" ] && [ -z "$${WORKFLOW_CONTROLLER_IMAGE:-}" ]; then \
		$(MAKE) --no-print-directory package-workflow-controller-image-archive \
			OUT_FILE="$$workflow_controller_image" \
			WORKFLOWS_VERSION="$$workflows_version" \
			WORKFLOW_CONTROLLER_BASE_IMAGE="$${WORKFLOW_CONTROLLER_BASE_IMAGE:-quay.io/argoproj/workflow-controller:$$workflows_version}"; \
		WORKFLOW_CONTROLLER_IMAGE="$$workflow_controller_image"; \
		WORKFLOW_CONTROLLER_IMAGE_REFERENCE="$${WORKFLOW_CONTROLLER_IMAGE_REFERENCE:-$$workflow_controller_image_ref}"; \
	fi; \
	if [ -z "$${ARTIFACT_SERVER_IMAGE:-}" ]; then \
		src_image="$${ARTIFACT_SERVER_SOURCE_IMAGE:-}"; \
		$(MAKE) --no-print-directory package-artifact-server-image-archive \
			OUT_FILE="$$artifact_server_image" \
			REFERENCE_OUT_FILE="$$artifact_server_reference_file" \
			ARTIFACT_SERVER_VERSION="$$artifact_server_version" \
			$${src_image:+ARTIFACT_SERVER_SOURCE_IMAGE="$$src_image"}; \
		ARTIFACT_SERVER_IMAGE_REFERENCE="$$(tr -d '\r\n' < "$$artifact_server_reference_file")"; \
	else \
		ARTIFACT_SERVER_IMAGE_REFERENCE="$${ARTIFACT_SERVER_IMAGE_REFERENCE:-}"; \
	fi; \
	if [ -z "$${DNS_IMAGE:-}" ]; then \
		bash ./scripts/package/export-dns-server-image-archive.sh \
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
		--host-packages-dir "$$host_packages_dir" \
		--host-packages-os-version "$$host_packages_os_version" \
		--artifact-server-image "$$artifact_server_image" \
		--artifact-server-image-reference "$${ARTIFACT_SERVER_IMAGE_REFERENCE}" \
		--artifact-server-version "$$artifact_server_version" \
		--dns-image "$$dns_image" \
		--dns-image-reference "$${DNS_IMAGE_REFERENCE}" \
		--dns-version "$$dns_version" \
		--k3s-version "$${K3S_VERSION}"; \
	if [ -n "$${LATEST_OUT_FILE:-}" ]; then set -- "$$@" --latest-out-file "$${LATEST_OUT_FILE}"; fi; \
	if [ -n "$${RELEASE_ID:-}" ]; then set -- "$$@" --release-id "$${RELEASE_ID}"; fi; \
	if [ -n "$${CHART_VERSION:-}" ]; then set -- "$$@" --chart-version "$${CHART_VERSION}"; fi; \
	if [ -n "$${SUPPORTED_UPGRADE_SOURCE:-}" ]; then set -- "$$@" --supported-upgrade-source "$${SUPPORTED_UPGRADE_SOURCE}"; fi; \
	if [ -n "$${SBOM_DIR:-}" ]; then set -- "$$@" --sbom-dir "$${SBOM_DIR}"; fi; \
	if [ -n "$${PROVENANCE_DIR:-}" ]; then set -- "$$@" --provenance-dir "$${PROVENANCE_DIR}"; fi; \
	if [ -n "$${NOTICES_DIR:-}" ]; then set -- "$$@" --notices-dir "$${NOTICES_DIR}"; fi; \
	if [ -n "$${TESTS_DIR:-}" ]; then set -- "$$@" --tests-dir "$${TESTS_DIR}"; fi; \
	if [ -n "$$workflows_version" ]; then set -- "$$@" --workflows-version "$$workflows_version"; fi; \
	if [ -n "$${WORKFLOW_CONTROLLER_IMAGE:-}" ]; then set -- "$$@" --workflow-controller-image "$${WORKFLOW_CONTROLLER_IMAGE}"; fi; \
	if [ -n "$${WORKFLOW_CONTROLLER_IMAGE_REFERENCE:-}" ]; then set -- "$$@" --workflow-controller-image-reference "$${WORKFLOW_CONTROLLER_IMAGE_REFERENCE}"; fi; \
	if [ -n "$${WORKFLOW_EXECUTOR_IMAGE:-}" ]; then set -- "$$@" --workflow-executor-image "$${WORKFLOW_EXECUTOR_IMAGE}"; fi; \
	if [ -n "$${WORKFLOW_EXECUTOR_IMAGE_REFERENCE:-}" ]; then set -- "$$@" --workflow-executor-image-reference "$${WORKFLOW_EXECUTOR_IMAGE_REFERENCE}"; fi; \
	if [ -n "$${WORKFLOWS_CRDS_DIR:-}" ]; then set -- "$$@" --workflows-crds-dir "$${WORKFLOWS_CRDS_DIR}"; fi; \
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
	if [ -z "$(DEV_REGISTRY)" ] || [ -z "$(DEV_REGISTRY_USER)" ] || [ -z "$(DEV_REGISTRY_TOKEN)" ]; then \
		echo "dev-registry-login: unified DEV_* tooling credentials are required:" >&2; \
		echo "  export DEV_REGISTRY=<tooling-registry-host>" >&2; \
		echo "  export DEV_IMAGE_REPO=<tooling-image-repo>" >&2; \
		echo "  export DEV_REGISTRY_USER=<username>" >&2; \
		echo "  export DEV_REGISTRY_TOKEN=<token>" >&2; \
		echo "  export DEV_REGISTRY_TLS_VERIFY=true|false" >&2; \
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
	echo "  # export unified DEV_REGISTRY / DEV_REGISTRY_USER / DEV_REGISTRY_TOKEN" >&2; \
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
			echo "Defaults:$$(whoami) env_keep += \"DEV_REGISTRY_USER DEV_REGISTRY_TOKEN DEV_IMAGE_TAG DEV_IMAGE_NAME DEV_REGISTRY DEV_IMAGE_REPO DEV_REGISTRY_TLS_VERIFY SERVICE_IMAGE_REGISTRY SERVICE_IMAGE_REPO SERVICE_IMAGE_NAME SERVICE_IMAGE_TAG OFFLINE_BUILD\""; \
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
