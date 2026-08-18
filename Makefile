BIN := dg
PKG := github.com/alcubie/delegator

.PHONY: build test vet fmt check clean watch

build:
	go build -o $(BIN) ./cmd/dg

test:
	go test ./...

watch:
	gotestsum --watch ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: fmt vet test

clean:
	rm -f $(BIN) coverage.out
