#!/bin/sh
set -eu

helpers=$(mktemp -d "${TMPDIR:-/tmp}/delegator-test-suite.XXXXXX")
trap 'rm -rf "$helpers"' EXIT HUP INT TERM

go build -o "$helpers" ./cmd/dg ./cmd/dg-fake-agent
DELEGATOR_TEST_DG="$helpers/dg" \
	DELEGATOR_TEST_FAKE_AGENT="$helpers/dg-fake-agent" \
	go test "$@" ./...
