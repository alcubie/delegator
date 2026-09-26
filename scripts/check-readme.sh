#!/bin/sh

# Check README command paths against a binary from the same tree. Full command
# syntax and flags are checked by the generated-reference target.
set -eu

[ "$#" -eq 1 ] || {
	printf '%s\n' "usage: check-readme.sh /path/to/dg" >&2
	exit 2
}

dg=$1
root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)

"$dg" --help >/dev/null
for command in \
	"agents --all" \
	"init --agent codex" \
	"config" \
	"config set" \
	"ticket" \
	"show --worktree-only" \
	"show --branch-only" \
	"accept" \
	"start" \
	"finish" \
	"restart" \
	"chat" \
	"cancel" \
	"version" \
	"agents add"
do
	# The words are fixed command names and flags from this file, not input.
	# shellcheck disable=SC2086
	"$dg" $command --help >/dev/null
done

for path in \
	LICENSE \
	PRIVACY.md \
	SECURITY.md \
	THIRD_PARTY_NOTICES.md \
	TRADEMARKS.md \
	docs/INTEGRATION_TESTS.md \
	docs/READ_THE_DOCS.md \
	docs/RELEASES.md \
	docs/TECHNICAL_DESIGN.md \
	docs/TOKEN_COSTS.md \
	docs/TOKEN_USAGE.md \
	docs/public/index.md
do
	[ -e "$root/$path" ] || {
		printf '%s\n' "README target does not exist: $path" >&2
		exit 1
	}
done

grep -F 'Status: development source only.' "$root/README.md" >/dev/null
grep -F 'A Git worktree isolates files but is not a sandbox.' "$root/README.md" >/dev/null
