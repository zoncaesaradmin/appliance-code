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

.PHONY: build test lint coverage image-local clean
build:
	@mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=$(if $(GOARCH),$(GOARCH),$(shell go env GOARCH)) go build -trimpath -ldflags="-s -w -X main.buildVersion=$${IMAGE_TAG:-development}" -o bin/channel-integration ./cmd/channel-integration
test:
	go test ./...
lint:
	go vet ./...
coverage:
	go test ./... -coverprofile=coverage.out
image-local: build
	$(call service_image_build,Containerfile,$(SERVICE_IMAGE_NAME),$(SERVICE_IMAGE_TAG),BUILD_VERSION=$${IMAGE_TAG:-development})
clean:
	rm -rf bin coverage.out
