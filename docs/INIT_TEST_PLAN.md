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

Expected: a numbered Goose/OpenCode menu ending in `Configure another agent…`,
with no PATH or COMMAND columns. Enter a number to save that agent; pressing
Enter without a number accepts the bracketed default. The closing text names
`dg` for the inbox, `dg ticket` for the first ticket, and the token cost.

## 2. Configure an agent that is not in the detected list

```bash
case_dir="$DG_TEST_ROOT/unlisted"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/goose"
make_fake "$case_dir/path/my-acp"
printf 'Use this path at the prompt: %s\n' "$case_dir/path/my-acp"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Expected: the menu contains Goose followed by `Configure another agent…`.
Enter `2`, then enter `mine` for the name and the printed `my-acp` path. Setup
completes with `mine` as the default. Confirm storage with:

```bash
PATH="$case_dir/path" "$DG_TEST_DG" config get default_agent --data-dir "$case_dir/data"
```

## 3. Configure an agent when none are detected

```bash
case_dir="$DG_TEST_ROOT/custom"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/my-acp"
printf 'Use this path at the prompt: %s\n' "$case_dir/path/my-acp"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Expected: Delegator reports that no supported agent was found and goes directly
to `Agent name:` without showing the agent chooser. Enter `mine`, then enter the
printed absolute path ending in `my-acp`. Confirm storage with:

```bash
PATH="$case_dir/path" "$DG_TEST_DG" config get default_agent --data-dir "$case_dir/data"
```

## 4. Accept the Codex ACP installation prompt safely

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

Expected: Codex appears as needing its ACP adapter. Enter `1`, then answer `y`.
The fake `npm` creates `codex-acp`; no network or global installation occurs.
Codex is then stored as the default.

## 5. Decline installation and supply a Claude adapter path

```bash
case_dir="$DG_TEST_ROOT/claude"
mkdir -p "$case_dir/path" "$case_dir/manual" "$case_dir/data"
make_fake "$case_dir/path/claude"
make_fake "$case_dir/manual/claude-acp"
printf 'Use this path at the prompt: %s\n' "$case_dir/manual/claude-acp"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Expected: enter `1` for Claude, answer `n` to installation, then enter the
absolute path ending in `manual/claude-acp`. That path is saved and Claude
becomes the default.

## 6. Rerun onboarding

Run any completed case again with the same PATH and data directory. Expected:
the current default is marked in the menu and its number is the bracketed
default. Enter keeps it; entering another number replaces it. No ticket or
agent process is created by init.

## 7. Require a terminal unless an agent flag is supplied

```bash
case_dir="$DG_TEST_ROOT/non-interactive"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/goose"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data" </dev/null
PATH="$case_dir/path" "$DG_TEST_DG" init --agent goose --data-dir "$case_dir/data" </dev/null
```

Expected: the first command fails with guidance to use `--agent NAME`; it does
not list or select agents. The second command completes with Goose as the
default without prompting.

Remove all disposable state when finished:

```bash
rm -rf "$DG_TEST_ROOT"
```
