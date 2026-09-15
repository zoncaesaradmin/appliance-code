SHELL := /bin/bash

.PHONY: build test lint coverage clean image-local

include version.env

SERVICE_IMAGE_NAME ?= appliance-inference-runtime
SERVICE_IMAGE_TAG ?= v$(VLLM_VERSION)
BASE_IMAGE ?= $(VLLM_AMD64_IMAGE)
BUILD_ENGINE ?= buildah bud --pull-never
GOARCH ?= amd64

# Use the compiler shipped in dev-build; packaging must not download toolchains.
export GOTOOLCHAIN := local
export GOWORK := off

build:
	mkdir -p bin
	GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags='-s -w' -o bin/inference-manager ./cmd/inference-manager

test:
	GOWORK=off go test ./...

lint:
	GOWORK=off go vet ./...
	@fmt_files="$$(gofmt -l .)"; test -z "$$fmt_files" || { echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; }

coverage:
	GOWORK=off go test -count=1 -coverprofile=coverage.out ./...
	GOWORK=off go tool cover -func=coverage.out

clean:
	rm -rf bin coverage.out

image-local: build
	$(BUILD_ENGINE) --build-arg BASE_IMAGE=$(BASE_IMAGE) -t $(SERVICE_IMAGE_NAME):$(SERVICE_IMAGE_TAG) .
