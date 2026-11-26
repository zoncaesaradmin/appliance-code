SERVICE_IMAGE_NAME ?= appliance-auth-service
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

.PHONY: build test lint coverage clean
build:
	@mkdir -p bin
	GOWORK=off GOTOOLCHAIN=$${GOTOOLCHAIN:-auto} CGO_ENABLED=0 GOOS=linux GOARCH=$(if $(GOARCH),$(GOARCH),$(shell go env GOARCH)) go build -trimpath -ldflags="-s -w -X main.buildVersion=$${IMAGE_TAG:-development}" -o bin/auth-service ./cmd/auth-service
test:
	GOWORK=off go test ./...
lint:
	GOWORK=off go vet ./...
coverage:
	GOWORK=off go test ./...
clean:
	rm -rf bin coverage.out
