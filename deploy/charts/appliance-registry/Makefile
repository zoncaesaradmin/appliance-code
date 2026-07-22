SHELL := /bin/bash
RENDER_OUT := /tmp/appliance-registry-rendered.yaml
COVER_FILE := coverage.out

.PHONY: build lint template test coverage clean
build:
	GOWORK=off go build ./...
lint:
	helm lint .
template:
	helm template appliance-registry . --namespace appliance-system \
		--set networkPolicy.traefikNamespaceLabel.kubernetes\\.io/metadata\\.name=traefik > $(RENDER_OUT)
test:
	GOWORK=off go test -race ./...
coverage:
	GOWORK=off go test -race -coverprofile=$(COVER_FILE) ./...
	GOWORK=off go tool cover -func=$(COVER_FILE)
clean:
	rm -f $(RENDER_OUT) $(COVER_FILE)
