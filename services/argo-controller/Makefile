SHELL := /bin/bash

REPO_ROOT   := $(abspath $(CURDIR)/../..)
CHART_YAML  := $(REPO_ROOT)/deploy/charts/argo-workflows/Chart.yaml

ENGINE_BIN   ?= buildah
BUILD_ENGINE ?= $(ENGINE_BIN) bud
IMAGE_NAME   ?= appliance-argo-controller
ARGO_VERSION ?= $(shell sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$$/\1/p' $(CHART_YAML))
IMAGE_TAG    ?= $(ARGO_VERSION)
IMAGE_REF    := $(IMAGE_NAME):$(IMAGE_TAG)
BASE_IMAGE   ?= quay.io/argoproj/workflow-controller:$(ARGO_VERSION)

.PHONY: image-local

image-local:
	$(BUILD_ENGINE) \
		--build-arg BASE_IMAGE=$(BASE_IMAGE) \
		-f Containerfile \
		-t $(IMAGE_REF) \
		.
