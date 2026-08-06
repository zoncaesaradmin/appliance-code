SHELL := /bin/bash

REPO_ROOT   := $(abspath $(CURDIR)/../..)
CHART_YAML  := $(REPO_ROOT)/deploy/charts/appliance-registry/Chart.yaml

# Service image: name + chart appVersion tag. Registry/repo defaults live in
# build/service-image.mk (DEV_REGISTRY host + appliance-images).
# `make image` / `make image-local` come from that shared include.
# Bundle export re-annotates as registry.local/artifact-server:bundled (existing install
# OCI contract name; keep until a deliberate registry.local rename).
SERVICE_IMAGE_NAME ?= appliance-artifact-server
ARTIFACT_SERVER_VERSION ?= $(shell sed -n 's/^appVersion: *"\{0,1\}v\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$$/\1/p' $(CHART_YAML))
SERVICE_IMAGE_TAG ?= v$(ARTIFACT_SERVER_VERSION)
# Current upstream pull ref (implementation detail; not the appliance name).
BASE_IMAGE ?= ghcr.io/project-zot/zot-linux-amd64:v$(ARTIFACT_SERVER_VERSION)
# glibc base for the dynamically linked upstream binary (not Alpine/musl).
RUNTIME_BASE_IMAGE ?= debian:bookworm-slim
SERVICE_IMAGE_BUILD_ARGS = \
	--build-arg BASE_IMAGE=$(BASE_IMAGE) \
	--build-arg RUNTIME_BASE_IMAGE=$(RUNTIME_BASE_IMAGE)

include $(CURDIR)/../../build/service-image.mk
