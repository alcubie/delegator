#!/usr/bin/env bash

set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TEST_TMP=$(mktemp -d)
ORIGINAL_PATH=$PATH
REAL_UNAME=$(command -v uname)
cleanup() {
	status=$?
	if ((status != 0)) && [[ -n ${CASE_DIR:-} ]]; then
		printf '%s\n' '--- installer stdout ---' >&2
		[[ ! -f $CASE_DIR/stdout ]] || sed -n '1,80p' "$CASE_DIR/stdout" >&2
		printf '%s\n' '--- installer stderr ---' >&2
		[[ ! -f $CASE_DIR/stderr ]] || sed -n '1,80p' "$CASE_DIR/stderr" >&2
	fi
	chmod -R u+w "$TEST_TMP" 2>/dev/null || true
	rm -rf "$TEST_TMP"
	exit "$status"
}
trap cleanup EXIT

unset DG_VERSION DG_INSTALL_DIR DG_NON_INTERACTIVE

fail() {
	printf 'test-install: %s\n' "$*" >&2
	return 1
}

assert_contains() {
	grep -F "$2" "$1" >/dev/null || fail "$1 does not contain: $2"
}

assert_no_staging_file() {
	if compgen -G "$CASE_DIR/bin/.dg.*" >/dev/null; then
		fail "a staging file was left in $CASE_DIR/bin"
	fi
}

setup_case() {
	CASE_DIR="$TEST_TMP/$1"
	mkdir -p "$CASE_DIR/fake-bin" "$CASE_DIR/releases" "$CASE_DIR/home" \
		"$CASE_DIR/tmp" "$CASE_DIR/bin"
	CURL_LOG="$CASE_DIR/curl.log"
	: > "$CURL_LOG"

	cat > "$CASE_DIR/fake-bin/uname" <<'EOF'
#!/bin/sh
case ${1:-} in
	-s) printf '%s\n' "${FAKE_UNAME_SYSTEM:-Linux}" ;;
	-m) printf '%s\n' "${FAKE_UNAME_MACHINE:-x86_64}" ;;
	*) exec "$REAL_UNAME" "$@" ;;
esac
EOF

	cat > "$CASE_DIR/fake-bin/curl" <<'EOF'
#!/usr/bin/env bash
set -eu
output=
url=
while (($#)); do
	case $1 in
		--output|--header|--user-agent|--proto)
			output_arg=$1
			shift
			(($#)) || exit 2
			if [[ $output_arg == --output ]]; then output=$1; fi
			shift
			;;
		--fail|--location|--silent|--show-error|--tlsv1.2) shift ;;
		*) url=$1; shift ;;
	esac
done
[[ -n $url && -n $output ]] || exit 2
printf '%s\n' "$url" >> "$CURL_LOG"
if [[ $url == https://api.github.com/repos/alcubie/delegator/releases/latest ]]; then
	printf '{"tag_name":"v%s"}\n' "$FAKE_LATEST_VERSION" > "$output"
	exit 0
fi
name=${url##*/}
source_file="$FAKE_RELEASE_DIR/$name"
[[ -f $source_file ]] || exit 22
if [[ ${FAKE_CURL_MODE:-} == interrupt && $name == *.tar.gz ]]; then
	printf 'partial download' > "$output"
	exit 18
fi
cp "$source_file" "$output"
EOF
	chmod +x "$CASE_DIR/fake-bin/uname" "$CASE_DIR/fake-bin/curl"

	export CASE_DIR CURL_LOG REAL_UNAME
	export FAKE_RELEASE_DIR="$CASE_DIR/releases"
	export FAKE_LATEST_VERSION=1.2.3
	export FAKE_UNAME_SYSTEM=Linux
	export FAKE_UNAME_MACHINE=x86_64
	unset FAKE_CURL_MODE
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

make_release() {
	local version=$1 os=${2:-linux} arch=${3:-amd64}
	local payload="$CASE_DIR/payload-$version-$os-$arch"
	local archive="alcubi-delegator_${version}_${os}_${arch}.tar.gz"
	mkdir -p "$payload"
	cat > "$payload/dg" <<EOF
#!/bin/sh
[ "\${1:-}" = version ] || exit 2
printf '%s\\n' 'dg v$version'
EOF
	chmod +x "$payload/dg"
	tar -czf "$CASE_DIR/releases/$archive" -C "$payload" dg
	printf '%s  %s\n' "$(sha256 "$CASE_DIR/releases/$archive")" "$archive" \
		> "$CASE_DIR/releases/alcubi-delegator_${version}_checksums.txt"
}

run_installer() {
	PATH="$CASE_DIR/fake-bin:$ORIGINAL_PATH" \
		HOME="$CASE_DIR/home" TMPDIR="$CASE_DIR/tmp" \
		DG_INSTALL_DIR="$CASE_DIR/bin" DG_NON_INTERACTIVE=1 \
		sh "$ROOT/install.sh" "$@" > "$CASE_DIR/stdout" 2> "$CASE_DIR/stderr"
}

expect_failure() {
	if run_installer "$@"; then
		fail "installer unexpectedly succeeded"
	fi
}

test_successful_latest_install() {
	setup_case success
	make_release 1.2.3
	run_installer
	[[ $("$CASE_DIR/bin/dg" version) == 'dg v1.2.3' ]]
	assert_contains "$CASE_DIR/stdout" "Installed Alcubi Delegator (dg v1.2.3) at $CASE_DIR/bin/dg"
	assert_contains "$CURL_LOG" 'https://api.github.com/repos/alcubie/delegator/releases/latest'
	assert_contains "$CURL_LOG" '/v1.2.3/alcubi-delegator_1.2.3_checksums.txt'
	assert_contains "$CURL_LOG" '/v1.2.3/alcubi-delegator_1.2.3_linux_amd64.tar.gz'
	assert_no_staging_file
}

test_reinstall_same_and_newer() {
	setup_case reinstall
	make_release 1.2.3
	run_installer --version 1.2.3
	run_installer --version v1.2.3
	make_release 1.3.0
	run_installer --version=1.3.0
	[[ $("$CASE_DIR/bin/dg" version) == 'dg v1.3.0' ]]
	assert_no_staging_file
}

test_macos_arm64_install() {
	setup_case macos
	export FAKE_UNAME_SYSTEM=Darwin FAKE_UNAME_MACHINE=arm64
	make_release 2.0.0 darwin arm64
	run_installer --version 2.0.0
	[[ $("$CASE_DIR/bin/dg" version) == 'dg v2.0.0' ]]
	assert_contains "$CURL_LOG" 'alcubi-delegator_2.0.0_darwin_arm64.tar.gz'
}

test_unsupported_platform() {
	setup_case unsupported-os
	export FAKE_UNAME_SYSTEM=FreeBSD
	expect_failure --version 1.2.3
	assert_contains "$CASE_DIR/stderr" 'unsupported operating system: FreeBSD'
	[[ ! -s $CURL_LOG ]]

	setup_case unsupported-arch
	export FAKE_UNAME_SYSTEM=Linux FAKE_UNAME_MACHINE=riscv64
	expect_failure --version 1.2.3
	assert_contains "$CASE_DIR/stderr" 'unsupported architecture: riscv64'
	[[ ! -s $CURL_LOG ]]
}

test_checksum_failure_preserves_existing_binary() {
	setup_case checksum
	make_release 1.2.3
	printf '#!/bin/sh\nprintf old\\n\n' > "$CASE_DIR/bin/dg"
	cp "$CASE_DIR/bin/dg" "$CASE_DIR/original-dg"
	chmod +x "$CASE_DIR/bin/dg" "$CASE_DIR/original-dg"
	printf 'corruption' >> "$CASE_DIR/releases/alcubi-delegator_1.2.3_linux_amd64.tar.gz"
	expect_failure --version 1.2.3
	assert_contains "$CASE_DIR/stderr" 'checksum verification failed'
	cmp "$CASE_DIR/original-dg" "$CASE_DIR/bin/dg"
	assert_no_staging_file
}

test_interrupted_download_leaves_no_binary() {
	setup_case interrupted
	make_release 1.2.3
	export FAKE_CURL_MODE=interrupt
	expect_failure --version 1.2.3
	assert_contains "$CASE_DIR/stderr" 'has no archive for linux/amd64'
	[[ ! -e $CASE_DIR/bin/dg ]]
	assert_no_staging_file
}

test_unavailable_release_leaves_no_binary() {
	setup_case unavailable
	expect_failure --version 9.9.9
	assert_contains "$CASE_DIR/stderr" 'release v9.9.9 is unavailable'
	[[ ! -e $CASE_DIR/bin/dg ]]
	assert_no_staging_file
}

test_no_writable_destination() {
	setup_case unwritable
	make_release 1.2.3
	chmod 0500 "$CASE_DIR/bin"
	expect_failure --version 1.2.3
	assert_contains "$CASE_DIR/stderr" 'install directory is not writable'
	[[ ! -e $CASE_DIR/bin/dg ]]
}

tests=(
	test_successful_latest_install
	test_reinstall_same_and_newer
	test_macos_arm64_install
	test_unsupported_platform
	test_checksum_failure_preserves_existing_binary
	test_interrupted_download_leaves_no_binary
	test_unavailable_release_leaves_no_binary
	test_no_writable_destination
)

for test_name in "${tests[@]}"; do
	"$test_name"
	printf 'ok - %s\n' "$test_name"
done
