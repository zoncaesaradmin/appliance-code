SHELL := /bin/bash

.PHONY: build test lint coverage clean image-local

include version.env

.DEFAULT_GOAL := build
INFERENCE_ENGINE ?= ollama
GOARCH ?= amd64
SERVICE_IMAGE_NAME ?= appliance-inference-runtime-$(INFERENCE_ENGINE)-$(GOARCH)
VERSION ?= dev-$(shell git rev-parse --short HEAD)-$(shell date -u +%Y%m%d%H%M%S)
VERSION := $(VERSION)
ifeq ($(INFERENCE_ENGINE)/$(GOARCH),ollama/amd64)
OLLAMA_VERSION := $(shell sed -n 's/^appVersion: *"\{0,1\}\([^"[:space:]]*\)"\{0,1\}[[:space:]]*$$/\1/p' ../../deploy/charts/appliance-inference/Chart.yaml)
BASE_IMAGE ?= docker.io/ollama/ollama:$(OLLAMA_VERSION)
else ifeq ($(INFERENCE_ENGINE)/$(GOARCH),vllm/amd64)
BASE_IMAGE ?= $(VLLM_AMD64_IMAGE)
else ifeq ($(INFERENCE_ENGINE)/$(GOARCH),vllm/arm64)
BASE_IMAGE ?= $(VLLM_ARM64_IMAGE)
endif

SERVICE_IMAGE_BUILD_ARGS = --arch $(GOARCH) --build-arg BASE_IMAGE=$(BASE_IMAGE)
include ../../build/service-image.mk

.PHONY: image-ollama-amd64 image-vllm-amd64 image-vllm-arm64
image-ollama-amd64:
	$(MAKE) image INFERENCE_ENGINE=ollama GOARCH=amd64

image-vllm-amd64:
	$(MAKE) image INFERENCE_ENGINE=vllm GOARCH=amd64

image-vllm-arm64:
	$(MAKE) image INFERENCE_ENGINE=vllm GOARCH=arm64

.PHONY: check-image-base
check-image-base:
	@test -n "$(BASE_IMAGE)" || { echo 'Unsupported runtime; select ollama/amd64, vllm/amd64, or vllm/arm64' >&2; exit 1; }
	@case "$(OFFLINE_BUILD)" in 1|true|TRUE|yes|YES|on|ON) \
		case "$(BASE_IMAGE)" in docker.io/*|ghcr.io/*) \
			echo 'Offline inference builds require BASE_IMAGE pointing to the seeded LAN image or a preloaded local image' >&2; exit 1 ;; esac ;; esac

image image-local: check-image-base build

# Use the compiler shipped in dev-build; packaging must not download toolchains.
export GOTOOLCHAIN := local
export GOWORK := off

build:
	mkdir -p bin
	GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags='-s -w' -o bin/inference-manager ./cmd/inference-manager

test:
	GOWORK=off go test ./...
	bash tests/image-targets.sh

lint:
	GOWORK=off go vet ./...
	@fmt_files="$$(gofmt -l .)"; test -z "$$fmt_files" || { echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; }

coverage:
	GOWORK=off go test -count=1 -coverprofile=coverage.out ./...
	GOWORK=off go tool cover -func=coverage.out

clean:
	rm -rf bin coverage.out
