# Alcubi Delegator

Alcubi Delegator gives coding-agent work an ordered queue, a separate Git
worktree for each ticket, and one inbox for review.

## Release status and platforms

**Status: development source only.** There is no published release, active
installer, stable version, or production support. The installer in this
repository is for a future release and cannot install Delegator until release
artifacts exist. It is tested distribution code, not a command that new users
can use today.

The source has implementations and release targets for Linux, macOS, and
Windows on `amd64` and `arm64`. The future shell installer targets Linux and
macOS on those architectures. These are development targets, not a guarantee
of support for a machine, agent, or agent version.

Alcubi is the house brand under which Matthew McCormick, an individual,
publishes this software. Alcubi Delegator and Delegator are provisional names
pending clearance. See the [trademark policy](TRADEMARKS.md).

## What Delegator does

Delegator:

- keeps tickets from multiple Git repositories in one ordered queue;
- starts a selected coding agent through the Agent Client Protocol (ACP);
- creates one branch and Git worktree for each run;
- records the agent session, result, commit, log, and reported token usage; and
- keeps completed work in `READY` until a person reviews and accepts it.

Delegator does not write the change, judge the result, merge the branch, run a
deployment, or provide a security boundary around the agent. Those actions
belong to the agent, the user, and the repository's own tools.

> **A Git worktree isolates files but is not a sandbox.** The agent runs with
> your user permissions and can reach paths, credentials, processes, and
> networks outside its worktree.

## Prerequisites

To use the current development source, you need:

- Git on `PATH` and an existing Git repository with at least one commit;
- Go 1.26 and Make to build `dg`; and
- an installed and authenticated ACP agent command.

The ACP command can be a separate adapter from the agent's normal terminal
command. Guided setup can offer to install the Claude or Codex adapter with
`npm`, so that path also needs Node.js and `npm`. Other agents have their own
installation and account requirements.

Python, MkDocs, `goimports`, `staticcheck`, GoReleaser, and authenticated test
agents are maintainer dependencies. They are not user prerequisites.

## Install and verify the development build

Review and build a source commit. There is no release archive to install yet.

```sh
git clone https://github.com/alcubie/delegator.git
cd delegator
make install
command -v dg
dg version
dg --help
```

`make install` uses the Go installation directory. If `command -v dg` prints
nothing, add the directory reported by `go env GOBIN`, or `$(go env
GOPATH)/bin` when `GOBIN` is empty, to `PATH`.

## Five-minute workflow

First inspect the built-in agent registry. Entries marked as missing are known
to Delegator but are not available on the current `PATH`.

```sh
dg agents --all
dg init
dg config
```

`dg init` discovers available agents and asks which one will run new tickets.
For non-interactive setup, use a name shown by `dg agents`:

```sh
dg init --agent codex
```

Then enter an existing Git repository. The new ticket starts automatically
when the queue has capacity.

```sh
cd /path/to/your/repository
ticket_id=$(dg ticket "Add a health check" \
  "Add a health-check endpoint and tests. Do not change authentication.")
printf 'Created ticket %s\n' "$ticket_id"
dg
```

Run `dg` again to read the inbox. When the ticket is `READY`, inspect its
record, worktree, commit, and diff. Run the repository's tests before merging.

```sh
dg show "$ticket_id"
worktree=$(dg show "$ticket_id" --worktree-only)
branch=$(dg show "$ticket_id" --branch-only)
git -C "$worktree" status --short
git -C "$worktree" show --stat --oneline HEAD
git diff HEAD..."$branch"
```

In the primary checkout, merge or squash the reviewed branch. `dg accept`
checks that the work is in the current `HEAD`, marks the ticket `DONE`, removes
its worktree, and starts more queued work when capacity is available.

```sh
git merge "$branch"
dg accept "$ticket_id"
dg
```

Do not use `dg accept --force` as a normal review step. It skips the merge and
clean-worktree checks and can discard uncommitted work.

## Ticket state model

The normal path is:

```text
QUEUED -> RUNNING -> READY -> DONE
```

| State | Meaning | How it changes |
| --- | --- | --- |
| `QUEUED` | The ticket waits for capacity and completed dependencies. | A supervisor claims it automatically. |
| `RUNNING` | An agent works in the ticket worktree. | `dg finish` makes it `READY`; an error, timeout, or lost supervisor makes it `FAILED`. |
| `READY` | The agent reported a commit and the ticket waits for review. | Merge the work, then use `dg accept` to make it `DONE`. |
| `FAILED` | A run ended without a successful `dg finish`. | Use `dg restart`, or continue with `dg chat` and then record a commit with `dg finish`. |
| `DONE` | A person accepted merged work. | Terminal state. |
| `CANCELLED` | A person closed work with `dg cancel`. | Terminal state; its worktree is kept for inspection. |

`RUNNING` and `READY` both use configured queue capacity. A dependency releases
its dependent ticket only after it becomes `DONE`.

## Configuration and local data

Use `dg config` to inspect settings and `dg config set` to change one. Settings
and the agent registry live in SQLite; there is no separate configuration
file.

| System | Default data directory |
| --- | --- |
| Linux and macOS | `$XDG_DATA_HOME/delegator`, or `~/.local/share/delegator` when `XDG_DATA_HOME` is empty |
| Windows | `%XDG_DATA_HOME%\delegator`, or `%LOCALAPPDATA%\delegator` when `XDG_DATA_HOME` is empty |

`--data-dir /absolute/path` selects another instance for one command. The data
directory contains `delegator.db`, ticket prose under `tickets/`, agent logs
under `runs/`, worktrees under `worktrees/`, and disposable per-project data
under `cache/projects/`. Ticket records and logs do not expire automatically.

## Registered and verified agents

The built-in registry has entries for Claude, Codex, Gemini, Goose, OpenCode,
GitHub Copilot, Cursor Agent, and Pi. A registry entry is configuration, not a
support guarantee. `dg agents` confirms only that an executable is on `PATH`.

Live integration tests in this repository record successful runs with these
versions:

| Agent | Last version recorded by its integration test |
| --- | --- |
| Claude | Claude Code 2.1.273 with `claude-agent-acp` 0.77.0 |
| Codex | Codex CLI 0.155.0 with `codex-acp` 1.11.0 |
| Goose | 1.50.1 |
| OpenCode | 1.18.31 |
| GitHub Copilot CLI | 1.0.86 |
| Cursor Agent | 2026.09.15-d2fe57e; ACP session load only |
| Pi | 0.85.1 with `pi-acp` 0.0.33 |

Gemini has a built-in registry entry but no live-agent integration test in the
repository. Newer versions, account settings, models, and provider changes can
behave differently. Custom ACP commands can also be registered, but Delegator
does not certify them. See [real-agent integration tests](docs/INTEGRATION_TESTS.md)
for commands, scope, and costs.

## Safety and limitations

- **A worktree is not a sandbox.** Unattended runs approve ACP tool requests,
  and the agent inherits the environment of `dg`.
- A cloud-connected agent can send source, ticket text, paths, tool results,
  and credentials to third parties. Read the [privacy notice](PRIVACY.md) and
  the agent provider's current terms before use.
- Agent runs can consume paid tokens. See [controlling token
  costs](docs/TOKEN_COSTS.md).
- Worktrees do not isolate ports, databases, credentials, caches, or other
  resources outside the checkout. Use `max_runs_per_project` when concurrent
  work in one repository can conflict.
- Delegator does not fetch, pull, push, merge, test, or deploy work for you.
- `dg cancel` keeps its worktree. Project caches, branches, commits, ticket
  records, and logs also need deliberate retention and cleanup decisions.
- Development builds have no stable compatibility or security-support promise.

## Troubleshooting

**No agent appears.** Run `dg agents --all`. Install the missing ACP command or
register an executable with `dg agents add`, then rerun `dg init`. An installed
agent CLI does not always include its ACP adapter.

**A ticket stays queued.** Run `dg` and check whether the queue is paused. Use
`dg start` to resume it. `RUNNING` and `READY` tickets hold capacity, and a
ticket can also wait for dependencies. Use `dg show ID` and `dg config` to
inspect them.

**A run failed.** Use `dg show ID` to read its result and run history. Use
`dg restart ID` to reuse its branch, worktree, and session. When terminal
resume is available, `dg chat ID` can continue the session.

**Acceptance is refused.** Merge or squash the recorded ticket commit into the
current `HEAD`, and make sure the ticket worktree is clean. Inspect both with
`dg show ID` before considering `--force`.

**`dg` is not on `PATH`.** Read the path printed by `make install` and compare
it with `go env GOBIN` and `go env GOPATH`.

For complete syntax and flags, use `dg help`, `dg COMMAND --help`, or the
[generated Alcubi Delegator CLI Reference](https://alcubi-delegator.readthedocs.io/en/latest/).

## Project documents and terms

- [Generated CLI reference source](docs/public/index.md)
- [Technical design](docs/TECHNICAL_DESIGN.md)
- [Release artifacts and procedure](docs/RELEASES.md)
- [Token-cost guidance](docs/TOKEN_COSTS.md) and [token accounting design](docs/TOKEN_USAGE.md)
- [Real-agent integration tests](docs/INTEGRATION_TESTS.md)
- [Privacy notice](PRIVACY.md) and [security policy](SECURITY.md)
- [FSL-1.1-ALv2 license](LICENSE), [third-party notices](THIRD_PARTY_NOTICES.md), and [trademark policy](TRADEMARKS.md)

Matthew McCormick publishes the software under the Functional Source License,
Version 1.1, ALv2 Future License (FSL-1.1-ALv2). Read the license for its exact
terms and future-license date. The software license does not grant permission
to present a modified product as official Alcubi software.

## Contributor commands

```sh
make build          # build ./dg
make test           # installer tests and ordinary Go tests
make check          # formatting, vet, lint, coverage, archives, and docs
make docs           # regenerate docs/public/reference from the Cobra tree
make docs-build     # build the documentation site
```

Install the pinned MkDocs environment as described in the [documentation
publishing guide](docs/READ_THE_DOCS.md). `make check` also needs `goimports`
and `staticcheck` on `PATH`.

`make integration` starts authenticated third-party agents and can consume
tokens. `make release` includes those tests and a publication-free artifact
build. Read the [integration-test setup](docs/INTEGRATION_TESTS.md) and
[release procedure](docs/RELEASES.md) before either command.
