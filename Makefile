BIN := dg
PKG := github.com/alcubie/delegator

.PHONY: build test vet fmt fmtcheck check clean watch

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

# fmtcheck reports bad formatting and stops. It does not change a file, so it is
# safe in a git hook.
fmtcheck:
	@bad=$$(gofmt -l .); \
	if [ -n "$$bad" ]; then \
		echo "gofmt is needed for:"; echo "$$bad"; exit 1; \
	fi

check: fmtcheck vet test

clean:
	rm -f $(BIN) coverage.out
