SHELL := /bin/bash

CHART_DIR   := .
RENDER_OUT  := /tmp/appliance-control-plane-rendered.yaml
COVER_FILE  := coverage.out

.PHONY: build lint template test verify coverage clean

## build: compile-check the chart policy test module
build:
	go build ./...

## lint: helm lint the chart (schema, required fields, best practices)
lint:
	@if ! command -v helm >/dev/null 2>&1; then \
		echo "helm not found on PATH (install: brew install helm, or see https://helm.sh/docs/intro/install/)"; \
		exit 1; \
	fi
	helm lint $(CHART_DIR)

## template: render the chart for manual inspection
template:
	@if ! command -v helm >/dev/null 2>&1; then \
		echo "helm not found on PATH (install: brew install helm, or see https://helm.sh/docs/intro/install/)"; \
		exit 1; \
	fi
	helm template appliance $(CHART_DIR) --namespace appliance \
		--set networkPolicy.traefikNamespaceLabel.kubernetes\\.io/metadata\\.name=traefik \
		> $(RENDER_OUT)
	@echo "rendered manifests written to $(RENDER_OUT)"

## test: run the Go-based structural policy tests (shells out to helm)
test:
	go test -race ./...

## verify: lint plus policy tests; this module's local pre-push gate.
## Actual install/restart/air-gap evidence needs a real K3s host, which this
## development environment does not have (see the Phase 0 note in
## docs/control-plane-v1-plan.md).
verify: lint test

## coverage: run tests with coverage and print a per-function summary
coverage:
	go test -race -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

## clean: remove local render/coverage artifacts
clean:
	rm -f $(RENDER_OUT) $(COVER_FILE)
