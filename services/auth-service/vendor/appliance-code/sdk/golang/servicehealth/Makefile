.PHONY: build test lint coverage clean

build:
	GOWORK=off go test -run '^$$' ./...

test:
	GOWORK=off go test ./...

lint:
	GOWORK=off go vet ./...

coverage:
	GOWORK=off go test ./...

clean:
	rm -f coverage.out
