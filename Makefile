BIN := dg
PKG := github.com/alcubie/delegator
NOTICE_FILES := LICENSE PRIVACY.md SECURITY.md TRADEMARKS.md THIRD_PARTY_NOTICES.md

# VERSION identifies the source tree through git describe: tag or commit, plus
# -dirty for uncommitted changes. dg version exposes it to users and clients.
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -X $(PKG)/internal/cli.Version=$(VERSION)

# COVER_MIN sets the minimum statement coverage for COVER_PKGS. Measure
# internal packages, excluding command entry points without dedicated tests.
COVER_MIN  := 75
COVER_PKGS := ./internal/...
DOCS_REFERENCE := docs/public/reference
MKDOCS ?= mkdocs
GORELEASER_VERSION := v2.17.1
POWERSHELL ?= pwsh
GORELEASER ?= go run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)
SNAPSHOT_VERSION := 0.0.0-snapshot-$(shell git rev-parse --short=7 HEAD)
VALIDATION_VERSION ?= 0.0.0-validate

.PHONY: build install install-test test integration release release-prepare release-tag release-verify release-upload-installer release-check release-snapshot release-validate release-build vet lint fmt fmtcheck check archivecheck clean watch cover coverhtml covercheck docs docs-build docs-serve docs-check

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/dg

# Install dg for local development and refresh goenv shims when available.
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/dg
	@command -v goenv >/dev/null && goenv rehash || true
	@echo "dg is at $$(command -v dg || echo '(not on the PATH)')"

install-test:
	./scripts/test-install.sh

.PHONY: install-test-windows
install-test-windows:
	$(POWERSHELL) -NoProfile -ExecutionPolicy Bypass -File scripts/test-install.ps1

test: install-test
	go test ./...

# Run only tagged TestIntegration tests. These use real agents and cost time
# and money; check compiles them without execution, while release runs them.
integration:
	@python3 scripts/test-integration.py $(INTEGRATION_ARGS)

# release is the gate before a release: every ordinary and integration test,
# followed by the same publish-free artifact build that a release uses.
release: check integration release-validate

# Run before tagging. Review and commit changes yourself; never modify a tag's
# source tree in the publication workflow.
release-prepare: docs
	git status --short -- $(DOCS_REFERENCE)
	git diff -- $(DOCS_REFERENCE)
	@test -z "$$(git status --porcelain -- $(DOCS_REFERENCE))" || \
		{ echo 'Review and commit the generated reference before creating a tag.'; exit 1; }

# Make treats positional arguments as goals. Consume the version as a no-op
# target only for these commands, and reject extra goals before doing any work.
ifneq ($(filter release-tag release-verify release-upload-installer,$(MAKECMDGOALS)),)
RELEASE_COMMAND := $(filter release-tag release-verify release-upload-installer,$(MAKECMDGOALS))
RELEASE_TAG_ARG := $(word 2,$(MAKECMDGOALS))
ifneq ($(MAKECMDGOALS),$(RELEASE_COMMAND) $(RELEASE_TAG_ARG))
$(error Usage: make $(RELEASE_COMMAND) v1.4.0)
endif
ifeq ($(filter v0% v1% v2% v3% v4% v5% v6% v7% v8% v9%,$(RELEASE_TAG_ARG)),)
$(error Usage: make $(RELEASE_COMMAND) v1.4.0)
endif
.PHONY: $(RELEASE_TAG_ARG)
$(RELEASE_TAG_ARG): ;
endif

# Create and push the release tag: make release-tag v1.4.0
release-tag:
	@test -z "$$(git status --porcelain)" || \
		{ echo 'Commit or stash changes before creating a release tag.'; exit 1; }
	git tag -a "$(RELEASE_TAG_ARG)" -m "Alcubi Delegator $(RELEASE_TAG_ARG)"
	git push origin "$(RELEASE_TAG_ARG)"

# Download and verify every release archive on Linux; keep files for inspection.
release-verify:
	@set -eu; \
	dir=$$(mktemp -d "$${TMPDIR:-/tmp}/delegator-release.XXXXXX"); \
	gh release download "$(RELEASE_TAG_ARG)" --repo alcubie/delegator --dir "$$dir"; \
	cd "$$dir"; \
	sha256sum --check --strict ./*_checksums.txt; \
	printf 'Verified release files: %s\n' "$$dir"

# Add the missing installer from the release's source without replacing assets.
release-upload-installer:
	@set -eu; \
	python3 scripts/check-release-tag.py "$(RELEASE_TAG_ARG)"; \
	dir=$$(mktemp -d "$${TMPDIR:-/tmp}/delegator-installer.XXXXXX"); \
	trap 'rm -rf "$$dir"' EXIT HUP INT TERM; \
	git show "$(RELEASE_TAG_ARG):install.sh" > "$$dir/install.sh"; \
	test -s "$$dir/install.sh"; \
	sh -n "$$dir/install.sh"; \
	gh release upload "$(RELEASE_TAG_ARG)" "$$dir/install.sh" --repo alcubie/delegator; \
	gh release download "$(RELEASE_TAG_ARG)" --pattern install.sh --dir "$$dir/downloaded" --repo alcubie/delegator; \
	cmp "$$dir/install.sh" "$$dir/downloaded/install.sh"

# Build an exact tag without giving the builder publication credentials.
release-build: release-check
	$(GORELEASER) release --clean --skip=publish
	go run ./cmd/dg-release-check -dist dist -version "$${RELEASE_TAG#v}"

# The pinned tool is invoked through the Go module cache. Both builds use
# --snapshot, which disables every publisher and requires no credentials.
release-check:
	$(GORELEASER) check

release-snapshot: release-check
	RELEASE_VERSION=$(SNAPSHOT_VERSION) $(GORELEASER) release --snapshot --clean

release-validate: release-check
	RELEASE_VERSION=$(VALIDATION_VERSION) $(GORELEASER) release --snapshot --clean
	go run ./cmd/dg-release-check -dist dist -version $(VALIDATION_VERSION)

watch:
	gotestsum --watch ./...

# Vet ordinary and integration tests without running paid agent tests.
#
# Cross-build cmd/dg for Windows. Do not build ./... for Windows:
# internal/testfix contains Unix-specific shell and process-group fixtures.
vet:
	go vet ./...
	go vet -tags integration ./...
	GOOS=windows go build -o /dev/null ./cmd/dg

# Run all staticcheck checks, including package comments (ST1000) and
# initialism capitalization (ST1003). Install
# honnef.co/go/tools/cmd/staticcheck to run this target.
lint:
	staticcheck -checks=all ./...

# Format Go code and update imports with golang.org/x/tools/cmd/goimports.
fmt:
	goimports -l -w .

# Report formatting errors without modifying files, suitable for commit hooks.
fmtcheck:
	@bad=$$(goimports -l .); \
	if [ -n "$$bad" ]; then \
		echo "goimports is needed for:"; echo "$$bad"; exit 1; \
	fi

# Run tests with coverage for COVER_PKGS and enforce COVER_MIN. Coverage
# measures executed statements, not assertion quality; use it to find untested
# code.
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
	@set -eu; generated=$$(mktemp -d); site=$$(mktemp -d); \
	trap 'rm -rf "$$generated" "$$site"' EXIT HUP INT TERM; \
	go run ./cmd/dg-docs --output-dir "$$generated"; \
	diff -ru $(DOCS_REFERENCE) "$$generated"; \
	$(MKDOCS) build --strict --clean --site-dir "$$site"

# Show statement coverage per function. Execution does not prove assertions
# checked the behavior.
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

# Show line coverage in a browser: green for executed lines, red for
# unexecuted lines.
coverhtml: cover
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

check: install-test archivecheck fmtcheck vet lint covercheck docs-check
	python3 scripts/test-integration-output.py

clean:
	rm -f $(BIN) coverage.out coverage.html
