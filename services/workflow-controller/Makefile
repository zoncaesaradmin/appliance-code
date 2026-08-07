SHELL := /bin/bash

REPO_ROOT   := $(abspath $(CURDIR)/../..)
CHART_YAML  := $(REPO_ROOT)/deploy/charts/appliance-workflows/Chart.yaml

# Service image: name + chart appVersion tag. Registry/repo defaults live in
# build/service-image.mk (DEV_REGISTRY host + appliance-images).
# `make image` / `make image-local` come from that shared include.
SERVICE_IMAGE_NAME ?= appliance-workflow-controller
WORKFLOWS_VERSION ?= $(shell sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$$/\1/p' $(CHART_YAML))
SERVICE_IMAGE_TAG ?= $(WORKFLOWS_VERSION)
BASE_IMAGE ?= quay.io/argoproj/workflow-controller:$(WORKFLOWS_VERSION)
SERVICE_IMAGE_BUILD_ARGS = \
	--build-arg BASE_IMAGE=$(BASE_IMAGE)
ifneq ($(strip $(RUNTIME_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg RUNTIME_IMAGE=$(RUNTIME_IMAGE)
endif
ifneq ($(strip $(RUNTIME_PREBAKED)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg RUNTIME_PREBAKED=$(RUNTIME_PREBAKED)
endif

include $(CURDIR)/../../build/service-image.mk
