# `dg init` manual test plan

This plan exercises onboarding without changing the normal Delegator instance,
launching an agent, or installing a package from the network. Run it from the
repository root in an interactive terminal.

## Prepare an isolated preview

```bash
make install
DG_TEST_DG=$(goenv which dg)
DG_TEST_ROOT=$(mktemp -d)

make_fake() {
  printf '#!/bin/sh\nexit 0\n' > "$1"
  chmod +x "$1"
}
```

Each case gets its own PATH and data directory. The absolute `DG_TEST_DG` path
bypasses the goenv shim, which needs `bash` on PATH.

## 1. Choose between detected agents

```bash
case_dir="$DG_TEST_ROOT/choose"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/goose"
make_fake "$case_dir/path/opencode"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Expected: a short Goose/OpenCode menu ending in `Configure another agent…`, no
PATH or COMMAND columns, Up/Down moves the marker, and Enter saves the
highlighted agent. The closing text names `dg` for the inbox, `dg ticket` for
the first ticket, and the token cost.

## 2. Configure an unrecognized ACP command

```bash
case_dir="$DG_TEST_ROOT/custom"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/my-acp"
printf 'Use this path at the prompt: %s\n' "$case_dir/path/my-acp"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Expected: no known agent is detected. Press Enter on the sole `Configure
another agent…` choice, then enter `mine` for the name and the printed absolute
path ending in `my-acp` for the executable. Confirm storage with:

```bash
PATH="$case_dir/path" "$DG_TEST_DG" config get default_agent --data-dir "$case_dir/data"
```

## 3. Accept the Codex ACP installation prompt safely

```bash
case_dir="$DG_TEST_ROOT/codex"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/codex"
export DG_TEST_BIN="$case_dir/path"
printf '%s\n' '#!/bin/sh' \
  'printf "#!/bin/sh\nexit 0\n" > "$DG_TEST_BIN/codex-acp"' \
  '/bin/chmod +x "$DG_TEST_BIN/codex-acp"' > "$case_dir/path/npm"
chmod +x "$case_dir/path/npm"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Expected: Codex appears as needing its ACP adapter. Select it and answer `y`.
The fake `npm` creates `codex-acp`; no network or global installation occurs.
Codex is then stored as the default.

## 4. Decline installation and supply a Claude adapter path

```bash
case_dir="$DG_TEST_ROOT/claude"
mkdir -p "$case_dir/path" "$case_dir/manual" "$case_dir/data"
make_fake "$case_dir/path/claude"
make_fake "$case_dir/manual/claude-acp"
printf 'Use this path at the prompt: %s\n' "$case_dir/manual/claude-acp"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Expected: select Claude, answer `n` to installation, then enter the absolute
path ending in `manual/claude-acp`. That path is saved and Claude becomes the
default.

## 5. Rerun onboarding

Run any completed case again with the same PATH and data directory. Expected:
the current default is marked in the menu; Enter keeps it, while Up/Down and
Enter explicitly replace it. No ticket or agent process is created by init.

Remove all disposable state when finished:

```bash
rm -rf "$DG_TEST_ROOT"
```
