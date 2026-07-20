.PHONY: build check fmt lint test vet

build:
	go build -trimpath -o bin/leetcode-solver ./cmd/leetcode-solver

fmt:
	gofmt -s -w $$(find . -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

test:
	go test -race -count=1 -coverprofile=coverage.out ./...

lint:
	golangci-lint run ./...

check: vet test build
