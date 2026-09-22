BIN := dg
PKG := github.com/alcubie/delegator
NOTICE_FILES := LICENSE PRIVACY.md SECURITY.md TRADEMARKS.md THIRD_PARTY_NOTICES.md

# VERSION is what dg version writes, and a program that starts dg reads it to
# know which binary it found. git describe names the tag when the tree is one,
# the commit when it is not, and adds -dirty when the tree holds changes that
# are not committed, so a binary always says where it came from.
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -X $(PKG)/internal/cli.Version=$(VERSION)

# COVER_MIN is the smallest coverage that a commit can have. COVER_PKGS says
# which packages the number applies to. cmd/dg is not in the list: a package
# with no test file counts as 0 percent, and it goes into the total, so the
# command layer would stop each commit before it has its own tests.
COVER_MIN  := 75
COVER_PKGS := ./internal/...
DOCS_REFERENCE := docs/public/reference
MKDOCS ?= mkdocs

.PHONY: build install test integration release vet lint fmt fmtcheck check archivecheck clean watch cover coverhtml covercheck docs docs-build docs-serve docs-check

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/dg

# install puts dg on the PATH of the person, so the work can use delegator while
# it builds delegator. goenv keeps a shim for each program, and it makes the one
# for a new program at a rehash.
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/dg
	@command -v goenv >/dev/null && goenv rehash || true
	@echo "dg is at $$(command -v dg || echo '(not on the PATH)')"

test:
	go test ./...

# integration runs only the TestIntegration tests behind the build tag
# "integration": each one drives a real agent and costs money and time, so
# check compiles them and release runs them. The tag adds their files to the
# build, and -run keeps the ordinary tests out of this target.
integration:
	go test -tags integration -timeout 15m -run '^TestIntegration' -v ./...

# release is the gate before a release: everything check does, and then the
# integration tests. It will grow the goreleaser build and the installer; for
# now it is the one command that runs every test the repository has.
release: check integration

watch:
	gotestsum --watch ./...

# vet also compiles the tests behind the integration tag without running them.
# The tag keeps those files out of every ordinary build, so without this a
# change to an adapter could break one and nobody would know until release.
#
# The last line builds dg for Windows, which the desktop GUI targets. It names
# cmd/dg and not ./..., because internal/testfix starts a shell with Setsid and
# signals a process group, and it is a package and not a test file, so ./...
# reaches it. The output goes nowhere: what this asks is whether the build is
# there, and go build takes /dev/null for that.
vet:
	go vet ./...
	go vet -tags integration ./...
	GOOS=windows go build -o /dev/null ./cmd/dg

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

# archivecheck makes the source archive from the tracked working-tree files,
# then checks the project-policy material that every source or binary
# distribution must carry. A future binary packager must use NOTICE_FILES too.
archivecheck:
	@archive=$$(mktemp); \
	trap 'rm -f "$$archive"' EXIT HUP INT TERM; \
	git ls-files | tar -T - -cf "$$archive"; \
	for file in $(NOTICE_FILES); do \
		if ! tar -tf "$$archive" "$$file" >/dev/null 2>&1; then \
			echo "archivecheck: source archive has no $$file"; exit 1; \
		fi; \
	done

# docs regenerates the checked-in command reference from Cobra's command tree.
# Removing the destination first also removes pages for commands that no longer
# exist. dg-docs constructs cli.Root directly; it never runs dg or opens an
# instance data directory.
docs:
	rm -rf $(DOCS_REFERENCE)
	go run ./cmd/dg-docs --output-dir $(DOCS_REFERENCE)

docs-build:
	$(MKDOCS) build --strict --clean

docs-serve:
	$(MKDOCS) serve

# docs-check uses temporary destinations so that CI neither changes the
# working tree nor leaves a built site behind. diff catches added, removed and
# changed pages before MkDocs checks navigation and links.
docs-check:
	@generated=$$(mktemp -d); site=$$(mktemp -d); \
	trap 'rm -rf "$$generated" "$$site"' EXIT HUP INT TERM; \
	go run ./cmd/dg-docs --output-dir "$$generated"; \
	diff -ru $(DOCS_REFERENCE) "$$generated"; \
	$(MKDOCS) build --strict --clean --site-dir "$$site"

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

check: archivecheck fmtcheck vet lint covercheck docs-check

clean:
	rm -f $(BIN) coverage.out coverage.html
