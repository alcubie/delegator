BIN := dg
PKG := github.com/alcubie/delegator

# COVER_MIN is the smallest coverage that a commit can have. COVER_PKGS says
# which packages the number applies to. cmd/dg is not in the list: a package
# with no test file counts as 0 percent, and it goes into the total, so the
# command layer would stop each commit before it has its own tests.
COVER_MIN  := 60
COVER_PKGS := ./internal/...

.PHONY: build install test vet lint fmt fmtcheck check clean watch cover coverhtml covercheck

build:
	go build -o $(BIN) ./cmd/dg

# install puts dg on the PATH of the person, so the work can use delegator while
# it builds delegator. goenv keeps a shim for each program, and it makes the one
# for a new program at a rehash.
install:
	go install ./cmd/dg
	@command -v goenv >/dev/null && goenv rehash || true
	@echo "dg is at $$(command -v dg || echo '(not on the PATH)')"

test:
	go test ./...

watch:
	gotestsum --watch ./...

vet:
	go vet ./...

# lint runs each check that staticcheck has, and not only the ones that it has
# by default. The two that are not default earn their place: ST1000 asks each
# package for a package comment, and ST1003 asks an initialism for its capitals,
# so an id is an ID. staticcheck comes from
# honnef.co/go/tools/cmd/staticcheck.
lint:
	staticcheck -checks=all ./...

# fmt formats and fixes the imports. goimports does everything gofmt does, and
# it also adds an import a file needs and removes one it does not, which is most
# of the work when code moves between files. It comes from
# golang.org/x/tools/cmd/goimports.
fmt:
	goimports -l -w .

# fmtcheck reports bad formatting and stops. It does not change a file, so it is
# safe in a git hook.
fmtcheck:
	@bad=$$(goimports -l .); \
	if [ -n "$$bad" ]; then \
		echo "goimports is needed for:"; echo "$$bad"; exit 1; \
	fi

# covercheck stops the build if the coverage is less than COVER_MIN. It runs
# each test one time: -coverpkg says which packages to measure, and the list at
# the end says which tests to run.
#
# Read the number with care. It counts the statements that a test runs, and not
# the statements that a test examines. A test with no assertion gives the same
# number as a test that does the work. Use it to find code that no test touches.
covercheck:
	go test -coverprofile=coverage.out -coverpkg=$(COVER_PKGS) ./...
	@go tool cover -func=coverage.out | awk -v min=$(COVER_MIN) '\
		/^total:/ { seen = 1; got = $$3 + 0 } \
		END { \
			if (!seen) { print "covercheck: no total in the profile"; exit 1 } \
			if (got < min) { \
				printf "covercheck: coverage is %.1f%%, and the minimum is %d%%\n", got, min; \
				exit 1 \
			} \
			printf "covercheck: coverage is %.1f%%\n", got \
		}'

# cover shows how much of each function the tests run. It does not show how much
# of it the tests examine: a test with no assertion gives the same number as a
# test with one. Use it to find code that no test touches, and not as a target.
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# coverhtml makes a page that gives each line a colour: green if a test runs it,
# red if no test does.
coverhtml: cover
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

check: fmtcheck vet lint covercheck

clean:
	rm -f $(BIN) coverage.out coverage.html
