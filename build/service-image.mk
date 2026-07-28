# Shared service (pod) image build/push rules.
#
# Include from a service Makefile (typically after VERSION / build-args):
#
#   SERVICE_IMAGE_NAME ?= appliance-control-plane   # optional; else dir name
#   SERVICE_IMAGE_BUILD_ARGS = --build-arg VERSION=$(VERSION) ...
#   include ../../build/service-image.mk
#
# Defaults (overridable via env or `make image SERVICE_IMAGE_*=...`):
#   SERVICE_IMAGE_REGISTRY  <- host of DEV_REGISTRY (required; no hardcoded fallback)
#   SERVICE_IMAGE_REPO      <- appliance-images
#   SERVICE_IMAGE_NAME      <- $(notdir $(CURDIR)) if unset
#   SERVICE_IMAGE_TAG       <- $(VERSION) if unset
#
# Auth/TLS (same as make dev-shell):
#   DEV_REGISTRY_USER / DEV_REGISTRY_TOKEN / DEV_REGISTRY_TLS_VERIFY
#
# Do not reuse DEV_IMAGE_NAME/REPO — those name development-container/dev-build.

DEV_REGISTRY ?=
# Host only: strip any legacy path suffix from DEV_REGISTRY (host/repo/...).
SERVICE_IMAGE_REGISTRY ?= $(firstword $(subst /, ,$(DEV_REGISTRY)))
ifeq ($(strip $(SERVICE_IMAGE_REGISTRY)),)
$(error service-image.mk: SERVICE_IMAGE_REGISTRY is empty; set DEV_REGISTRY or SERVICE_IMAGE_REGISTRY)
endif

SERVICE_IMAGE_REPO ?= appliance-images

# Name: SERVICE_IMAGE_NAME from env, make CLI, or service Makefile (?=).
# If still unset, use the service directory name (e.g. controlplane, ui).
ifeq ($(origin SERVICE_IMAGE_NAME),undefined)
SERVICE_IMAGE_NAME := $(notdir $(CURDIR))
endif
ifeq ($(strip $(SERVICE_IMAGE_NAME)),)
$(error service-image.mk: SERVICE_IMAGE_NAME is empty)
endif

SERVICE_IMAGE_TAG ?= $(VERSION)
ifeq ($(strip $(SERVICE_IMAGE_TAG)),)
$(error service-image.mk: SERVICE_IMAGE_TAG or VERSION must be set)
endif

ENGINE_BIN ?= buildah
BUILD_ENGINE ?= $(ENGINE_BIN) bud
BUILD_CACHE_FLAGS ?=
ifeq ($(BUILD_NO_CACHE),1)
BUILD_CACHE_FLAGS += --no-cache
endif

SERVICE_IMAGE_REF := $(SERVICE_IMAGE_NAME):$(SERVICE_IMAGE_TAG)
SERVICE_IMAGE_REMOTE := $(SERVICE_IMAGE_REGISTRY)/$(SERVICE_IMAGE_REPO)/$(SERVICE_IMAGE_NAME)

DEV_REGISTRY_USER ?=
DEV_REGISTRY_TOKEN ?=
DEV_REGISTRY_TLS_VERIFY ?= true
SERVICE_IMAGE_TLS_FLAG :=
ifeq ($(filter false 0 no FALSE NO,$(DEV_REGISTRY_TLS_VERIFY)),$(DEV_REGISTRY_TLS_VERIFY))
SERVICE_IMAGE_TLS_FLAG := --tls-verify=false
endif

SERVICE_IMAGE_CONTAINERFILE ?= Containerfile
SERVICE_IMAGE_BUILD_ARGS ?=

.PHONY: image-local image

## image-local: build this service image into local storage (no push)
image-local:
	$(BUILD_ENGINE) $(BUILD_CACHE_FLAGS) \
		$(SERVICE_IMAGE_BUILD_ARGS) \
		-f $(SERVICE_IMAGE_CONTAINERFILE) \
		-t $(SERVICE_IMAGE_REF) \
		.

## image: build this service image and push to SERVICE_IMAGE_REMOTE
image:
	@if [ -z "$(DEV_REGISTRY_USER)" ] || [ -z "$(DEV_REGISTRY_TOKEN)" ]; then \
		echo "image: DEV_REGISTRY_USER and DEV_REGISTRY_TOKEN must both be set (never interactive):" >&2; \
		echo "  export DEV_REGISTRY_USER=<registry-username>" >&2; \
		echo "  export DEV_REGISTRY_TOKEN=<registry-token>" >&2; \
		exit 1; \
	fi
	$(BUILD_ENGINE) $(BUILD_CACHE_FLAGS) \
		$(SERVICE_IMAGE_BUILD_ARGS) \
		-f $(SERVICE_IMAGE_CONTAINERFILE) \
		-t $(SERVICE_IMAGE_REF) \
		.
	echo "$(DEV_REGISTRY_TOKEN)" | $(ENGINE_BIN) login $(SERVICE_IMAGE_TLS_FLAG) --username "$(DEV_REGISTRY_USER)" --password-stdin $(SERVICE_IMAGE_REGISTRY)
	$(ENGINE_BIN) tag $(SERVICE_IMAGE_REF) $(SERVICE_IMAGE_REMOTE):$(SERVICE_IMAGE_TAG)
	$(ENGINE_BIN) push $(SERVICE_IMAGE_TLS_FLAG) $(SERVICE_IMAGE_REMOTE):$(SERVICE_IMAGE_TAG)
	@echo "image: pushed $(SERVICE_IMAGE_REMOTE):$(SERVICE_IMAGE_TAG)"
	@if [ "$(SERVICE_IMAGE_TAG)" != "latest" ]; then \
		$(ENGINE_BIN) tag $(SERVICE_IMAGE_REF) $(SERVICE_IMAGE_REMOTE):latest; \
		$(ENGINE_BIN) push $(SERVICE_IMAGE_TLS_FLAG) $(SERVICE_IMAGE_REMOTE):latest; \
		echo "image: pushed $(SERVICE_IMAGE_REMOTE):latest"; \
	fi
