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

The node paths below refer to the numbers in `docs/INIT_WORKFLOW.mermaid`.
Transcripts use `/tmp/dg-init` in place of the random `DG_TEST_ROOT` value.
Text following a prompt is what the tester types; pressing Enter with no text
is shown as `(press Enter)`.

## 1. Choose between detected agents

```bash
case_dir="$DG_TEST_ROOT/choose"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/goose"
make_fake "$case_dir/path/opencode"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Diagram path: `01 → 02 No → 06 → 07 → 08 Yes → 11 OpenCode → 13 No → 05 → 26 Yes → 27`.

Expected screen when selecting OpenCode with `2`:

```text
Welcome to Alcubi Delegator.
Current default agent: none
Choose your default agent:
  1. Goose
  2. OpenCode
Selection [1] (q to cancel): 2

Setup complete.

Default agent: OpenCode

The default agent will be used to execute ticket tasks with your user permissions.
Agent runs can use tokens from your plan.

Next steps:
    dg          view the inbox
    dg ticket   create your first ticket
```

## 2. Report that no supported agent was found

```bash
case_dir="$DG_TEST_ROOT/missing"
mkdir -p "$case_dir/path" "$case_dir/data"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Diagram path: `01 → 02 No → 06 → 07 → 08 No → 09 → 10`.

Expected screen—there is no chooser or custom-agent prompt:

```text
Welcome to Alcubi Delegator.
Current default agent: none

No supported Agent Client Protocol (ACP) command was found.
Register one with:
    dg agents add NAME --command /path/to/executable

Setup is incomplete until a default agent is selected.
```

## 3. Accept the Codex ACP command installation prompt safely

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

Diagram path: `01 → 02 No → 06 → 07 → 08 Yes → 11 Codex → 13 Yes → 14 → 15 Yes → 16 → 17 Yes → 19 Yes → 20 → 05 → 26 Yes → 27`.

Expected screen; the fake `npm` creates `codex-acp`, so nothing is installed
from the network:

```text
Welcome to Alcubi Delegator.
Current default agent: none
Choose your default agent:
  1. Codex
Selection [1] (q to cancel): 1

Delegator uses Agent Client Protocol (ACP) to communicate with Codex while it runs ticket tasks.
The required ACP command codex-acp is not available.
Install it now with npm install -g @agentclientprotocol/codex-acp? [y/N] y
Running npm install -g @agentclientprotocol/codex-acp

Setup complete.

Default agent: Codex

The default agent will be used to execute ticket tasks with your user permissions.
Agent runs can use tokens from your plan.

Next steps:
    dg          view the inbox
    dg ticket   create your first ticket
```

## 4. Decline installation and supply a Claude ACP command path

```bash
case_dir="$DG_TEST_ROOT/claude"
mkdir -p "$case_dir/path" "$case_dir/manual" "$case_dir/data"
make_fake "$case_dir/path/claude"
make_fake "$case_dir/manual/claude-acp"
printf 'Use this path at the prompt: %s\n' "$case_dir/manual/claude-acp"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data"
```

Diagram path: `01 → 02 No → 06 → 07 → 08 Yes → 11 Claude → 13 Yes → 14 → 15 No → 21 → 22 Yes → 24 Yes → 25 → 20 → 05 → 26 Yes → 27`.

Expected screen:

```text
Welcome to Alcubi Delegator.
Current default agent: none
Choose your default agent:
  1. Claude
Selection [1] (q to cancel): 1

Delegator uses Agent Client Protocol (ACP) to communicate with Claude while it runs ticket tasks.
The required ACP command claude-agent-acp is not available.
Install it now with npm install -g @agentclientprotocol/claude-agent-acp? [y/N] n
ACP command or absolute path (leave blank to cancel): /tmp/dg-init/claude/manual/claude-acp

Setup complete.

Default agent: Claude

The default agent will be used to execute ticket tasks with your user permissions.
Agent runs can use tokens from your plan.

Next steps:
    dg          view the inbox
    dg ticket   create your first ticket
```

## 5. Rerun onboarding

Run case 1 again with the same PATH and data directory.

Diagram path: the same as case 1; node `11` defaults to the current agent.

Expected screen when pressing Enter without a number:

```text
Welcome to Alcubi Delegator.
Current default agent: OpenCode
Choose your default agent:
  1. Goose
  2. OpenCode (current)
Selection [2] (q to cancel): (press Enter)

Setup complete.

Default agent: OpenCode

The default agent will be used to execute ticket tasks with your user permissions.
Agent runs can use tokens from your plan.

Next steps:
    dg          view the inbox
    dg ticket   create your first ticket
```

Run it once more and enter `1` to replace OpenCode with Goose. The screen is
the same through node `11`; the completion screen names Goose. Neither run
creates a ticket or starts an agent process.

## 6. Require a terminal unless an agent flag is supplied

```bash
case_dir="$DG_TEST_ROOT/non-interactive"
mkdir -p "$case_dir/path" "$case_dir/data"
make_fake "$case_dir/path/goose"
PATH="$case_dir/path" "$DG_TEST_DG" init --data-dir "$case_dir/data" </dev/null
PATH="$case_dir/path" "$DG_TEST_DG" init --agent goose --data-dir "$case_dir/data" </dev/null
```

First-command diagram path: `01 → 02 No → 06 terminal requirement fails`.

First-command screen—there is no agent discovery or chooser output:

```text
delegator: dg init requires an interactive terminal; use dg init --agent NAME for scripted setup
```

Second-command diagram path: `01 → 02 Yes → 03 → 04 No → 05 → 26 Yes → 27`.

Second-command screen:

```text
Welcome to Alcubi Delegator.
Current default agent: none

Setup complete.

Default agent: Goose

The default agent will be used to execute ticket tasks with your user permissions.
Agent runs can use tokens from your plan.

Next steps:
    dg          view the inbox
    dg ticket   create your first ticket
```

Remove all disposable state when finished:

```bash
rm -rf "$DG_TEST_ROOT"
```
