SHELL := /bin/bash

.PHONY: build test lint coverage clean image-local image image-ollama-amd64 image-vllm-amd64 image-vllm-arm64

include version.env

.DEFAULT_GOAL := build
# GOARCH is required (amd64|arm64). Prefer explicit GOARCH; else TARGET_ARCH.
# There is no amd64 default — empty fails closed so arch misses surface early.
GOARCH ?= $(TARGET_ARCH)
SERVICE_IMAGE_NAME ?= appliance-inference-manager
VERSION ?= dev-$(shell git rev-parse --short HEAD)-$(shell date -u +%Y%m%d%H%M%S)
VERSION := $(VERSION)
BASE_IMAGE ?= docker.io/library/alpine:3.24.1

SERVICE_IMAGE_BUILD_ARGS = --arch $(GOARCH) --build-arg BASE_IMAGE=$(BASE_IMAGE)
include ../../build/service-image.mk

# Legacy aliases keep packaging/docs call sites working; they all build the
# same thin manager image (engine selection is pack metadata + runtime image).
image-ollama-amd64 image-vllm-amd64 image-vllm-arm64: image

.PHONY: require-goarch check-image-base
require-goarch:
	@case "$(GOARCH)" in \
	  amd64|arm64) ;; \
	  *) echo 'inference-manager: GOARCH or TARGET_ARCH is required (amd64|arm64); no default' >&2; exit 2 ;; \
	esac

check-image-base: require-goarch
	@test -n "$(BASE_IMAGE)" || { echo 'BASE_IMAGE is required' >&2; exit 1; }
	@case "$(OFFLINE_BUILD)" in 1|true|TRUE|yes|YES|on|ON) \
		case "$(BASE_IMAGE)" in docker.io/*|ghcr.io/*) \
			echo 'Offline inference-manager builds require BASE_IMAGE pointing to the seeded LAN image or a preloaded local image' >&2; exit 1 ;; esac ;; esac

image image-local: check-image-base build

# Packaging on the seeded build host must not download toolchains.
ifdef OFFLINE_BUILD
export GOTOOLCHAIN := local
endif
export GOWORK := off

build: require-goarch
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
