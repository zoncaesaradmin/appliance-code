.PHONY: build test lint coverage verify clean

build:
	go build ./...

test:
	go test -race ./...

lint:
	go vet ./...
	@test -z "$(gofmt -l .)" || { echo "geosnapshot SDK contains unformatted Go files" >&2; exit 1; }

coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

verify: lint test

clean:
	rm -f coverage.out
