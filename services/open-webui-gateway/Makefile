SHELL := /bin/bash
# Local verification follows the host architecture; release packaging passes
# TARGET_ARCH explicitly and the exporter fails closed for unsupported values.
GOARCH ?= $(if $(TARGET_ARCH),$(TARGET_ARCH),$(shell go env GOARCH))
SERVICE_IMAGE_NAME ?= appliance-open-webui-gateway
VERSION ?= dev
BASE_IMAGE ?= docker.io/library/alpine:3.24.1
SERVICE_IMAGE_BUILD_ARGS = --arch $(GOARCH) --build-arg BASE_IMAGE=$(BASE_IMAGE)
include ../../build/service-image.mk
.PHONY: build test lint coverage image image-local
build:
	@case "$(GOARCH)" in amd64|arm64) ;; *) echo 'GOARCH or TARGET_ARCH is required' >&2; exit 2;; esac
	mkdir -p bin
	GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=$(GOARCH) go build -trimpath -ldflags='-s -w' -o bin/open-webui-gateway .
test:
	GOWORK=off go test ./...
lint:
	GOWORK=off go vet ./...
	@test -z "$$(gofmt -l .)"
coverage:
	GOWORK=off go test -count=1 -coverprofile=coverage.out ./...
	GOWORK=off go tool cover -func=coverage.out
image image-local: build
