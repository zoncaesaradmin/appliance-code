SHELL := /bin/bash

COVER_FILE := coverage.out

.PHONY: build test lint coverage verify clean

## build: compile-check every package (a client library has no binary to build)
build:
	go build ./...

## test: run the SDK's unit tests
test:
	go test -race ./...

## lint: vet plus gofmt
lint:
	go vet ./...
	@fmt_files="$$(gofmt -l .)"; \
	if [ -n "$$fmt_files" ]; then \
		echo "gofmt found unformatted files:"; echo "$$fmt_files"; exit 1; \
	fi

## coverage: run tests with coverage and print a per-function summary
coverage:
	go test -race -coverprofile=$(COVER_FILE) ./...
	go tool cover -func=$(COVER_FILE)

## verify: this module's local pre-push gate
verify: lint test

## clean: remove local coverage artifacts
clean:
	rm -f $(COVER_FILE)
