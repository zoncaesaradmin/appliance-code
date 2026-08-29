SHELL := /bin/bash
RENDER_OUT := /tmp/appliance-inference-rendered.yaml
COVER_FILE := coverage.out

.PHONY: build lint template test coverage clean
build:
	GOWORK=off go build ./...
lint:
	helm lint .
template:
	helm template appliance-inference . --namespace inference \
		--set image.digest=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
		> $(RENDER_OUT)
test:
	GOWORK=off go test -race ./...
coverage:
	GOWORK=off go test -race -count=1 -coverprofile=$(COVER_FILE) ./...
	GOWORK=off go tool cover -func=$(COVER_FILE)
clean:
	rm -f $(RENDER_OUT) $(COVER_FILE)
