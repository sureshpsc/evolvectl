.PHONY: test vet fmt build

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

build:
	go build -o bin/evolvectl ./cmd/evolvectl

race:
	go test -race ./...
