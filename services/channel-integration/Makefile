SERVICE_IMAGE_NAME ?= appliance-channel-integration
SERVICE_IMAGE_TAG ?= local
GOARCH ?= $(TARGET_ARCH)
ifneq ($(strip $(GO_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg GO_IMAGE=$(GO_IMAGE)
endif
ifneq ($(strip $(RUNTIME_IMAGE)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg RUNTIME_IMAGE=$(RUNTIME_IMAGE)
endif
ifneq ($(strip $(RUNTIME_PREBAKED)),)
SERVICE_IMAGE_BUILD_ARGS += --build-arg RUNTIME_PREBAKED=$(RUNTIME_PREBAKED)
endif
include ../../build/service-image.mk
SERVICE_IMAGE_EXTRA_BUILD_ARGS += --build-arg BUILD_VERSION=$(SERVICE_IMAGE_TAG)

# This service is packaged as an independent module. Never inherit the
# appliance root workspace during the release build: that workspace may select
# a newer patch toolchain and trigger a public download. Online builds retain
# Go's normal toolchain policy; offline builds fail closed instead of attempting
# a toolchain fetch.
ifdef OFFLINE_BUILD
GO_TOOLCHAIN_ENV := GOTOOLCHAIN=local
else
GO_TOOLCHAIN_ENV :=
endif
ifdef OFFLINE_BUILD
export GOTOOLCHAIN := local
endif

.PHONY: build test lint coverage image-local clean
build:
	@mkdir -p bin
	GOWORK=off $(GO_TOOLCHAIN_ENV) CGO_ENABLED=0 GOOS=linux GOARCH=$(if $(GOARCH),$(GOARCH),$(shell go env GOARCH)) go build -trimpath -ldflags="-s -w -X main.buildVersion=$${IMAGE_TAG:-development}" -o bin/channel-integration ./cmd/channel-integration
test:
	go test ./...
lint:
	go vet ./...
coverage:
	go test ./... -coverprofile=coverage.out
clean:
	rm -rf bin coverage.out
