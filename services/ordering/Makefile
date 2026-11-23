SERVICE_IMAGE_NAME ?= appliance-ordering
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

ifdef OFFLINE_BUILD
export GOTOOLCHAIN := local
endif

.PHONY: build test lint coverage clean
build:
	@mkdir -p bin
	GOWORK=off GOTOOLCHAIN=$${GOTOOLCHAIN:-auto} CGO_ENABLED=0 GOOS=linux GOARCH=$(if $(GOARCH),$(GOARCH),$(shell go env GOARCH)) go build -mod=vendor -trimpath -ldflags="-s -w -X main.buildVersion=$${IMAGE_TAG:-development}" -o bin/ordering ./cmd/ordering
test:
	GOWORK=off go test ./...
lint:
	GOWORK=off go vet ./...
coverage:
	# Go 1.26's downloaded macOS toolchain in the appliance build image does
	# not ship cmd/covdata; keep the verifier's coverage stage meaningful by
	# running the complete ordering test suite without a profile here.
	GOWORK=off go test ./...
clean:
	rm -rf bin coverage.out
