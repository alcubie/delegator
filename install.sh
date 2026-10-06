#!/bin/sh

# Install Alcubi Delegator's dg executable from a published GitHub release.
# The downloaded binary is not extracted or installed until its SHA-256 digest
# matches the canonical checksum file from the same release.
set -eu

umask 077

release_api="https://api.github.com/repos/alcubie/delegator/releases/latest"
release_root="https://github.com/alcubie/delegator/releases/download"
requested_version=${DG_VERSION:-}
install_dir=${DG_INSTALL_DIR:-}
non_interactive=${DG_NON_INTERACTIVE:-0}
work_dir=
stage_file=

usage() {
	cat <<'EOF'
Install Alcubi Delegator.

Usage: install.sh [--version VERSION] [--install-dir DIRECTORY]
                  [--non-interactive]

Options:
  --version VERSION       Install this release (the latest stable by default).
  --install-dir DIRECTORY Install dg in this directory.
  --non-interactive       Never prompt (the installer never uses sudo).
  -h, --help              Show this help.

The same settings can be supplied with DG_VERSION, DG_INSTALL_DIR, and
DG_NON_INTERACTIVE=1. Command-line options take precedence.
EOF
}

fail() {
	printf '%s\n' "Alcubi Delegator installer: $*" >&2
	exit 1
}

cleanup() {
	if [ -n "$stage_file" ]; then
		rm -f "$stage_file"
	fi
	if [ -n "$work_dir" ]; then
		rm -rf "$work_dir"
	fi
}

trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

while [ "$#" -gt 0 ]; do
	case $1 in
		--version)
			[ "$#" -ge 2 ] || fail "--version requires a value"
			requested_version=$2
			shift 2
			;;
		--version=*)
			requested_version=${1#*=}
			shift
			;;
		--install-dir)
			[ "$#" -ge 2 ] || fail "--install-dir requires a value"
			install_dir=$2
			shift 2
			;;
		--install-dir=*)
			install_dir=${1#*=}
			shift
			;;
		--non-interactive)
			non_interactive=1
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		--)
			shift
			break
			;;
		*) fail "unknown option: $1" ;;
	esac
done
[ "$#" -eq 0 ] || fail "unexpected argument: $1"

case $non_interactive in
	0|false|no|'') ;;
	1|true|yes) non_interactive=1 ;;
	*) fail "DG_NON_INTERACTIVE must be 0 or 1" ;;
esac

for command_name in curl grep sed awk tar mktemp mkdir cp chmod mv uname; do
	command -v "$command_name" >/dev/null 2>&1 || fail "required command not found: $command_name"
done

case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "unsupported operating system: $(uname -s)" ;;
esac

case $(uname -m) in
	x86_64|amd64) arch=amd64 ;;
	aarch64|arm64) arch=arm64 ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
esac

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/alcubi-delegator.XXXXXX") || \
	fail "could not create a temporary directory"

download() {
	url=$1
	output=$2
	curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
		--header 'Accept: application/vnd.github+json' \
		--user-agent 'alcubi-delegator-installer' \
		--output "$output" "$url"
}

validate_version() {
	printf '%s\n' "$1" | grep -Eq \
		'^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
}

if [ -z "$requested_version" ]; then
	metadata="$work_dir/latest.json"
	if ! download "$release_api" "$metadata"; then
		fail "could not determine the latest stable release"
	fi
	tag=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$metadata" | sed -n '1p')
	[ -n "$tag" ] || fail "latest release metadata has no tag_name"
	requested_version=${tag#v}
	validate_version "$requested_version" || fail "latest release has an invalid version: $tag"
	case ${requested_version%%+*} in
		*-*) fail "latest release is not stable: $tag" ;;
	esac
else
	requested_version=${requested_version#v}
	validate_version "$requested_version" || fail "invalid version: $requested_version"
fi

version=$requested_version
tag=v$version
prefix=delegator
base="$release_root/$tag"
checksums_path="$work_dir/checksums.txt"

if ! download "$base/${prefix}_${version}_checksums.txt" "$checksums_path"; then
	# Published releases from before the filename change retain their assets.
	prefix=alcubi-delegator
	if ! download "$base/${prefix}_${version}_checksums.txt" "$checksums_path"; then
		fail "release $tag is unavailable or has no canonical checksum file"
	fi
fi
archive="${prefix}_${version}_${os}_${arch}.tar.gz"
archive_path="$work_dir/$archive"
if ! download "$base/$archive" "$archive_path"; then
	fail "release $tag has no archive for $os/$arch"
fi

if ! expected=$(awk -v name="$archive" '
	NF == 2 && $2 == name && length($1) == 64 && $1 ~ /^[0-9a-f]+$/ {
		print $1
		matches++
	}
	END { if (matches != 1) exit 1 }
' "$checksums_path"); then
	fail "canonical checksum file has no unique valid entry for $archive"
fi

if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$archive_path" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$archive_path" | awk '{print $1}')
else
	fail "required SHA-256 command not found (sha256sum or shasum)"
fi
[ "$actual" = "$expected" ] || fail "checksum verification failed for $archive"

extract_dir="$work_dir/extract"
mkdir "$extract_dir" || fail "could not prepare the extraction directory"
tar -xzf "$archive_path" -C "$extract_dir" dg || fail "could not extract dg from $archive"
[ -f "$extract_dir/dg" ] && [ -x "$extract_dir/dg" ] || \
	fail "release archive does not contain an executable dg"

reported_version=$("$extract_dir/dg" version) || fail "downloaded dg could not report its version"
[ "$reported_version" = "dg v$version" ] || \
	fail "downloaded dg reported '$reported_version', expected 'dg v$version'"

if [ -n "$install_dir" ]; then
	mkdir -p "$install_dir" || fail "could not create install directory: $install_dir"
	[ -d "$install_dir" ] && [ -w "$install_dir" ] && [ -x "$install_dir" ] || \
		fail "install directory is not writable: $install_dir"
else
	selected=
	if [ -n "${HOME:-}" ]; then
		for candidate in "$HOME/.local/bin" "$HOME/bin"; do
			if [ -d "$candidate" ] && [ -w "$candidate" ] && [ -x "$candidate" ]; then
				selected=$candidate
				break
			fi
		done
	fi
	if [ -z "$selected" ] && [ -d /usr/local/bin ] && [ -w /usr/local/bin ] && [ -x /usr/local/bin ]; then
		selected=/usr/local/bin
	fi
	if [ -z "$selected" ] && [ -n "${HOME:-}" ] && [ -d "$HOME" ] && [ -w "$HOME" ]; then
		candidate="$HOME/.local/bin"
		if mkdir -p "$candidate" && [ -w "$candidate" ] && [ -x "$candidate" ]; then
			selected=$candidate
		fi
	fi
	[ -n "$selected" ] || fail "no writable standard install directory; use --install-dir"
	install_dir=$selected
fi

install_dir=$(cd "$install_dir" && pwd -P) || fail "could not resolve install directory"
destination="$install_dir/dg"
stage_file=$(mktemp "$install_dir/.dg.XXXXXX") || \
	fail "could not create a staging file in $install_dir"
cp "$extract_dir/dg" "$stage_file" || fail "could not stage dg in $install_dir"
chmod 0755 "$stage_file" || fail "could not make the staged dg executable"
mv -f "$stage_file" "$destination" || fail "could not install dg at $destination"
stage_file=

printf '%s\n' "Installed Alcubi Delegator ($reported_version) at $destination"

configure_path() {
	case :${PATH:-}: in
		*:"$install_dir":*) return ;;
	esac

	# Quote the directory as shell data, including spaces and single quotes.
	quoted_dir=$(printf '%s' "$install_dir" | sed "s/'/'\\\\''/g")
	path_command="export PATH='$quoted_dir':\"\$PATH\""
	profile=
	if [ -n "${HOME:-}" ]; then
		case ${SHELL##*/} in
			bash)
				if [ "$os" = darwin ]; then
					# Bash reads only the first existing login profile.
					profile=$HOME/.bash_profile
					if [ ! -f "$profile" ]; then
						if [ -f "$HOME/.bash_login" ]; then
							profile=$HOME/.bash_login
						elif [ -f "$HOME/.profile" ]; then
							profile=$HOME/.profile
						fi
					fi
				else
					profile=$HOME/.bashrc
				fi
				;;
			zsh) profile=${ZDOTDIR:-$HOME}/.zshrc ;;
		esac
	fi

	printf '\n'
	if [ -n "$profile" ]; then
		if grep -Fqx "$path_command" "$profile" 2>/dev/null; then
			printf '%s\n' "PATH is already configured in $profile."
		elif (printf '\n# Added by Alcubi Delegator.\n%s\n' "$path_command" >> "$profile") 2>/dev/null; then
			printf '%s\n' "Added $install_dir to PATH in $profile."
		else
			printf '%s\n' "Could not update $profile. Add this command to your shell configuration:"
			printf '    %s\n' "$path_command"
			profile=
		fi
	else
		printf '%s\n' 'Add this command to your shell configuration for future terminals:'
		printf '    %s\n' "$path_command"
	fi
	if [ -n "$profile" ]; then
		printf '%s\n' 'New terminal windows will pick this up automatically.'
	fi
	printf '\n%s\n\n    %s\n\n' 'To use dg in this terminal now, run:' "$path_command"
}

# SHELL identifies the user's shell, not the sh running this piped installer.
SHELL=${SHELL:-}
configure_path

show_init_command() {
	printf '%s\n' "To set up Alcubi Delegator later, run:" "    dg init"
}

# stdin contains this script in the documented curl-to-sh invocation. Open the
# controlling terminal separately so that every stream used by dg init still
# belongs to that terminal. Failure to open /dev/tty is the normal unattended
# case, which installs the command without starting onboarding.
if [ "$non_interactive" = 1 ]; then
	show_init_command
	exit 0
fi
if ! ( : <>/dev/tty ) 2>/dev/null; then
	show_init_command
	exit 0
fi
exec 3<>/dev/tty

printf '\n' >&3
"$destination" init <&3 >&3 2>&3
