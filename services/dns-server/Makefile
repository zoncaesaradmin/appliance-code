SHELL := /bin/bash

REPO_ROOT   := $(abspath $(CURDIR)/../..)
CHART_YAML  := $(REPO_ROOT)/deploy/charts/appliance-dns/Chart.yaml

# Service image: name + chart appVersion tag. Registry/repo defaults live in
# build/service-image.mk (DEV_REGISTRY host + appliance-images).
# `make image` / `make image-local` come from that shared include.
# Bundle export re-annotates as registry.local/coredns:bundled (install OCI
# contract; upstream engine is CoreDNS).
SERVICE_IMAGE_NAME ?= appliance-dns-server
DNS_VERSION ?= $(shell sed -n 's/^appVersion: *"\{0,1\}v\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$$/\1/p' $(CHART_YAML))
SERVICE_IMAGE_TAG ?= v$(DNS_VERSION)
BASE_IMAGE ?= registry.k8s.io/coredns/coredns:v$(DNS_VERSION)
SERVICE_IMAGE_BUILD_ARGS = \
	--build-arg BASE_IMAGE=$(BASE_IMAGE)

include $(CURDIR)/../../build/service-image.mk
