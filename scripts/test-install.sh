#!/usr/bin/env bash

set -euo pipefail

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TEST_TMP=$(mktemp -d)
ORIGINAL_PATH=$PATH
REAL_UNAME=$(command -v uname)
REAL_PYTHON=$(python3 -c 'import sys; print(sys.executable)')
REAL_SH=$(command -v sh)
TEST_DG=
cleanup() {
	status=$?
	if ((status != 0)) && [[ -n ${CASE_DIR:-} ]]; then
		printf '%s\n' '--- installer stdout ---' >&2
		[[ ! -f $CASE_DIR/stdout ]] || sed -n '1,80p' "$CASE_DIR/stdout" >&2
		printf '%s\n' '--- installer stderr ---' >&2
		[[ ! -f $CASE_DIR/stderr ]] || sed -n '1,80p' "$CASE_DIR/stderr" >&2
		printf '%s\n' '--- installer terminal ---' >&2
		[[ ! -f $CASE_DIR/terminal-output ]] || sed -n '1,120p' "$CASE_DIR/terminal-output" >&2
	fi
	chmod -R u+w "$TEST_TMP" 2>/dev/null || true
	rm -rf "$TEST_TMP"
	exit "$status"
}
trap cleanup EXIT

unset DG_VERSION DG_INSTALL_DIR DG_NON_INTERACTIVE XDG_DATA_HOME

fail() {
	printf 'test-install: %s\n' "$*" >&2
	return 1
}

assert_contains() {
	grep -F "$2" "$1" >/dev/null || fail "$1 does not contain: $2"
}

assert_not_contains() {
	if grep -F "$2" "$1" >/dev/null; then
		fail "$1 unexpectedly contains: $2"
	fi
}

assert_line() {
	grep -Fx "$2" "$1" >/dev/null || fail "$1 has no exact line: $2"
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
	unset FAKE_CURL_MODE ZDOTDIR INSTALLER_TEST_PATH
	export SHELL=/bin/bash
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

make_release() {
	local version=$1 os=${2:-linux} arch=${3:-amd64} binary=${4:-}
	local prefix=${5:-delegator}
	local payload="$CASE_DIR/payload-$version-$os-$arch"
	local archive="${prefix}_${version}_${os}_${arch}.tar.gz"
	mkdir -p "$payload"
	if [[ -n $binary ]]; then
		cp "$binary" "$payload/dg"
	else
		cat > "$payload/dg" <<EOF
#!/bin/sh
[ "\${1:-}" = version ] || exit 2
printf '%s\\n' 'dg v$version'
EOF
	fi
	chmod +x "$payload/dg"
	tar -czf "$CASE_DIR/releases/$archive" -C "$payload" dg
	printf '%s  %s\n' "$(sha256 "$CASE_DIR/releases/$archive")" "$archive" \
		> "$CASE_DIR/releases/${prefix}_${version}_checksums.txt"
}

run_installer() {
	cat "$ROOT/install.sh" | PATH="${INSTALLER_TEST_PATH:-$CASE_DIR/fake-bin:$ORIGINAL_PATH}" \
		HOME="$CASE_DIR/home" TMPDIR="$CASE_DIR/tmp" \
		DG_INSTALL_DIR="$CASE_DIR/bin" DG_NON_INTERACTIVE=1 \
		sh -s -- "$@" > "$CASE_DIR/stdout" 2> "$CASE_DIR/stderr"
}

build_test_dg() {
	if [[ -n $TEST_DG ]]; then
		return
	fi
	TEST_DG="$TEST_TMP/dg-v1.2.3"
	CGO_ENABLED=0 go -C "$ROOT" build \
		-ldflags '-X github.com/alcubie/delegator/internal/cli.Version=v1.2.3' \
		-o "$TEST_DG" ./cmd/dg
}

isolate_path() {
	local name target
	for name in awk bash cat chmod cp grep gzip mkdir mktemp mv rm sed sh tar; do
		target=$(PATH="$ORIGINAL_PATH" command -v "$name") || fail "test command not found: $name"
		ln -sf "$target" "$CASE_DIR/fake-bin/$name"
	done
	for name in sha256sum shasum; do
		if target=$(PATH="$ORIGINAL_PATH" command -v "$name"); then
			ln -sf "$target" "$CASE_DIR/fake-bin/$name"
		fi
	done
}

run_piped_installer() {
	local mode=$1 answers=$2
	printf '%s' "$answers" > "$CASE_DIR/answers"
	PATH="$CASE_DIR/fake-bin" HOME="$CASE_DIR/home" TMPDIR="$CASE_DIR/tmp" \
		XDG_DATA_HOME="$CASE_DIR/data-home" DG_INSTALL_DIR="$CASE_DIR/bin" \
		DG_VERSION=1.2.3 DG_NON_INTERACTIVE=0 \
		"$REAL_PYTHON" "$ROOT/scripts/test-install-pty.py" \
		"$mode" "$CASE_DIR/answers" "$CASE_DIR/terminal-output" -- \
		"$REAL_SH" -c 'cat "$1" | sh' installer-test "$ROOT/install.sh"
}

make_onboarding_release() {
	build_test_dg
	make_release 1.2.3 linux amd64 "$TEST_DG"
	isolate_path
}

assert_no_tickets() {
	local tickets
	tickets=$(PATH="$CASE_DIR/fake-bin" XDG_DATA_HOME="$CASE_DIR/data-home" \
		"$CASE_DIR/bin/dg" list) || fail "installed dg could not list tickets"
	[[ -z $tickets ]] || fail "onboarding created a ticket without consent: $tickets"
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
	assert_contains "$CURL_LOG" '/v1.2.3/delegator_1.2.3_checksums.txt'
	assert_contains "$CURL_LOG" '/v1.2.3/delegator_1.2.3_linux_amd64.tar.gz'
	assert_no_staging_file
}

test_legacy_release_install() {
	setup_case legacy
	export FAKE_LATEST_VERSION=0.0.2
	make_release 0.0.2 linux amd64 '' alcubi-delegator
	run_installer
	[[ $("$CASE_DIR/bin/dg" version) == 'dg v0.0.2' ]]
	assert_contains "$CURL_LOG" '/v0.0.2/alcubi-delegator_0.0.2_checksums.txt'
	assert_contains "$CURL_LOG" '/v0.0.2/alcubi-delegator_0.0.2_linux_amd64.tar.gz'
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
	assert_contains "$CURL_LOG" 'delegator_2.0.0_darwin_arm64.tar.gz'
}

test_shell_path_configuration() {
	setup_case bash-path
	export FAKE_UNAME_SYSTEM=Darwin
	make_release 1.2.3 darwin
	run_installer
	assert_contains "$CASE_DIR/stdout" "Added $CASE_DIR/bin to PATH in $CASE_DIR/home/.bash_profile"
	assert_contains "$CASE_DIR/stdout" 'To use dg in this terminal now, run:'
	local resolved
	resolved=$(HOME="$CASE_DIR/home" bash --noprofile --norc -c '. "$HOME/.bash_profile"; command -v dg')
	[[ $resolved == "$CASE_DIR/bin/dg" ]]
	run_installer
	[[ $(grep -c '^export PATH=' "$CASE_DIR/home/.bash_profile") == 1 ]]
	assert_contains "$CASE_DIR/stdout" 'PATH is already configured'

	setup_case existing-profile
	export FAKE_UNAME_SYSTEM=Darwin
	printf '# Existing settings' > "$CASE_DIR/home/.profile"
	make_release 1.2.3 darwin
	run_installer
	[[ ! -e $CASE_DIR/home/.bash_profile ]]
	assert_line "$CASE_DIR/home/.profile" '# Existing settings'
	assert_contains "$CASE_DIR/home/.profile" 'export PATH='

	setup_case zsh-path
	export SHELL=/bin/zsh ZDOTDIR="$CASE_DIR/zsh-config"
	mkdir "$ZDOTDIR"
	make_release 1.2.3
	run_installer
	assert_contains "$ZDOTDIR/.zshrc" 'export PATH='
	[[ ! -e $CASE_DIR/home/.bashrc ]]

	setup_case already-on-path
	export INSTALLER_TEST_PATH="$CASE_DIR/fake-bin:$CASE_DIR/bin:$ORIGINAL_PATH"
	make_release 1.2.3
	run_installer
	[[ ! -e $CASE_DIR/home/.bashrc ]]
	assert_not_contains "$CASE_DIR/stdout" 'To use dg in this terminal now'
}

test_path_configuration_fallbacks() {
	setup_case unsupported-shell
	export SHELL=/bin/fish
	make_release 1.2.3
	run_installer
	[[ ! -e $CASE_DIR/home/.bashrc ]]
	assert_contains "$CASE_DIR/stdout" 'Add this command to your shell configuration'
	assert_not_contains "$CASE_DIR/stdout" 'New terminal windows will pick this up'

	setup_case unwritable-profile
	mkdir "$CASE_DIR/home/.bashrc"
	make_release 1.2.3
	run_installer
	assert_contains "$CASE_DIR/stdout" 'Could not update'
	assert_contains "$CASE_DIR/stdout" 'To use dg in this terminal now'
	[[ -x $CASE_DIR/bin/dg ]]
}

test_path_configuration_quotes_directory() {
	setup_case quoted-path
	make_release 1.2.3
	local destination="$CASE_DIR/a space ' and \$(touch unexpected)"
	run_installer --install-dir "$destination"
	local resolved
	resolved=$(HOME="$CASE_DIR/home" bash --noprofile --norc -c '. "$HOME/.bashrc"; command -v dg')
	[[ $resolved == "$destination/dg" ]]
	[[ ! -e unexpected ]]
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
	printf 'corruption' >> "$CASE_DIR/releases/delegator_1.2.3_linux_amd64.tar.gz"
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

test_piped_interactive_acceptance_runs_onboarding() {
	setup_case interactive-accept
	make_onboarding_release
	cat > "$CASE_DIR/fake-bin/goose" <<'EOF'
#!/bin/sh
exit 0
EOF
	chmod +x "$CASE_DIR/fake-bin/goose"

	run_piped_installer terminal $'\nn\n'
	assert_not_contains "$CASE_DIR/terminal-output" 'Set up Alcubi Delegator now?'
	assert_contains "$CASE_DIR/terminal-output" 'Welcome to Alcubi Delegator.'
	assert_contains "$CASE_DIR/terminal-output" 'Setup complete.'
	assert_contains "$CASE_DIR/terminal-output" 'Default agent: Goose'
	local selected
	selected=$(PATH="$CASE_DIR/fake-bin" XDG_DATA_HOME="$CASE_DIR/data-home" \
		"$CASE_DIR/bin/dg" config get default_agent)
	[[ $selected == goose ]] || fail "default agent is $selected, want goose"
}

test_piped_interactive_decline_leaves_setup_incomplete() {
	setup_case interactive-decline
	make_onboarding_release
	cat > "$CASE_DIR/fake-bin/goose" <<'EOF'
#!/bin/sh
exit 0
EOF
	chmod +x "$CASE_DIR/fake-bin/goose"

	run_piped_installer terminal $'q\n'
	assert_contains "$CASE_DIR/terminal-output" 'Welcome to Alcubi Delegator.'
	assert_contains "$CASE_DIR/terminal-output" 'Choose your default agent:'
	assert_contains "$CASE_DIR/terminal-output" \
		'Setup is incomplete until a default agent is selected.'
	assert_not_contains "$CASE_DIR/terminal-output" 'Setup complete.'
	local selected
	selected=$(PATH="$CASE_DIR/fake-bin" XDG_DATA_HOME="$CASE_DIR/data-home" \
		"$CASE_DIR/bin/dg" config get default_agent)
	[[ -z $selected ]] || fail "declined onboarding selected $selected"
	assert_no_tickets
}

test_piped_onboarding_reports_a_missing_agent() {
	setup_case missing-agent
	make_onboarding_release

	run_piped_installer terminal ''
	assert_contains "$CASE_DIR/terminal-output" \
		'No supported Agent Client Protocol (ACP) command was found.'
	assert_contains "$CASE_DIR/terminal-output" \
		'dg agents add NAME --command /path/to/executable'
	assert_contains "$CASE_DIR/terminal-output" \
		'Setup is incomplete until a default agent is selected.'
}

test_onboarding_consent_is_not_first_ticket_consent() {
	setup_case first-ticket-consent
	make_onboarding_release
	cat > "$CASE_DIR/fake-bin/goose" <<'EOF'
#!/bin/sh
exit 0
EOF
	chmod +x "$CASE_DIR/fake-bin/goose"

	# Control-D closes terminal input after selecting Goose. Choosing an agent
	# during dg init is never consent to create or run a ticket.
	run_piped_installer terminal $'\nn\n\004'
	assert_contains "$CASE_DIR/terminal-output" 'Setup complete.'
	assert_no_tickets
}

test_closed_terminal_input_does_not_select_an_agent() {
	setup_case terminal-closed
	make_onboarding_release
	cat > "$CASE_DIR/fake-bin/goose" <<'EOF'
#!/bin/sh
exit 0
EOF
	chmod +x "$CASE_DIR/fake-bin/goose"

	run_piped_installer terminal $'\004'
	assert_contains "$CASE_DIR/terminal-output" 'Welcome to Alcubi Delegator.'
	assert_contains "$CASE_DIR/terminal-output" \
		'Setup is incomplete until a default agent is selected.'
	assert_not_contains "$CASE_DIR/terminal-output" 'Setup complete.'
	local selected
	selected=$(PATH="$CASE_DIR/fake-bin" XDG_DATA_HOME="$CASE_DIR/data-home" \
		"$CASE_DIR/bin/dg" config get default_agent)
	[[ -z $selected ]] || fail "closed terminal input selected $selected"
	assert_no_tickets
}

test_piped_install_without_a_terminal_prints_init_command() {
	setup_case no-terminal
	make_onboarding_release

	run_piped_installer no-terminal ''
	assert_not_contains "$CASE_DIR/terminal-output" 'Set up Alcubi Delegator now?'
	assert_line "$CASE_DIR/terminal-output" 'To set up Alcubi Delegator later, run:'
	assert_line "$CASE_DIR/terminal-output" '    dg init'
	[[ ! -e $CASE_DIR/data-home/delegator/delegator.db ]] || \
		fail 'an unattended installation created Delegator data'
}

tests=(
	test_successful_latest_install
	test_legacy_release_install
	test_reinstall_same_and_newer
	test_macos_arm64_install
	test_shell_path_configuration
	test_path_configuration_fallbacks
	test_path_configuration_quotes_directory
	test_unsupported_platform
	test_checksum_failure_preserves_existing_binary
	test_interrupted_download_leaves_no_binary
	test_unavailable_release_leaves_no_binary
	test_no_writable_destination
	test_piped_interactive_acceptance_runs_onboarding
	test_piped_interactive_decline_leaves_setup_incomplete
	test_piped_onboarding_reports_a_missing_agent
	test_onboarding_consent_is_not_first_ticket_consent
	test_closed_terminal_input_does_not_select_an_agent
	test_piped_install_without_a_terminal_prints_init_command
)

for test_name in "${tests[@]}"; do
	"$test_name"
	printf 'ok - %s\n' "$test_name"
done
