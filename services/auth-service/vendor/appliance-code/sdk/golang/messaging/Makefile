.PHONY: build test lint coverage verify clean
build:
	GOWORK=off go build ./...
test:
	GOWORK=off go test ./...
lint:
	GOWORK=off go vet ./...
	@test -z "$$(gofmt -l .)" || { echo "messaging SDK contains unformatted Go files" >&2; exit 1; }
coverage:
	GOWORK=off go test -race -count=1 -coverprofile=coverage.out ./...
	GOWORK=off go tool cover -func=coverage.out
verify: lint test
clean:
	rm -rf bin coverage.out
