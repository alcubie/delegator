# delegator

Delegate long tasks to a coding agent, then come back to one ordered inbox.

Delegator does one thing: it controls the queue and the isolation of the work. It does not
write code, do a diff, or examine the work. Those tasks belong to programs that you
select.

Status: in development. Nothing here is released yet.

Read the searchable [Alcubi Delegator CLI Reference](https://alcubi-delegator.readthedocs.io/en/latest/)
for the complete `dg` command syntax.

## License

Delegator is Fair Source (source available), not open source while a version is
protected. It is licensed under [FSL-1.1-ALv2](LICENSE). Its Competing Use
restriction prohibits making the software available to others in a commercial
product or service that substitutes for Delegator, substitutes for another
Alcubi offering that uses Delegator and exists when that version is made
available, or offers the same or substantially similar functionality. Each
version automatically becomes available under Apache 2.0 two years after
Matthew McCormick makes that version available. See the full license for the
precise terms and
[third-party notices](THIRD_PARTY_NOTICES.md) for separately licensed
dependencies. The code license does not grant permission to brand a fork as an
official product; see the separate [trademark policy](TRADEMARKS.md).

## Install

```
curl -fsSL https://alcubi.ai/delegator/install.sh | sh
```

## Questions

### A run costs more tokens than I expected. What can I do?

Give the agent a map of the repository so that it does not have to build one
by reading.

Almost all of what a run spends is the conversation being read back. Every
token that goes into the context is read again on every later call, so the
cost of a run grows with the square of its length, and the tokens that go in
early are the ones that are read the most times. An agent that opens a
repository it has never seen puts a lot in early: it reads whole files to
learn what is where, and then carries all of it to the end of the run.

Across seven runs of delegator on 2026-09-04, the reading each run did before
it changed a single line left between 17,000 and 91,000 tokens in the
context, and carrying that for the rest of the run cost between 21 and 48
percent of the whole run. The seven runs opened 75 different files between
them and only 13 of those were opened by four or more runs, so the runs were
not reading the same things: each was answering the same question, "what is
in this repository and where", from scratch.

Write the answer down once, in the file the agent already reads at the start
of a run. For claude that is CLAUDE.md at the root of the repository. Give it:

- one line per directory saying what belongs in it
- the commands to build, test and lint
- where the conventions of the project are written
- the few files that a person new to the code would open first

Keep it at the level of a directory rather than a file. A map that names files
is wrong as soon as a file is added, and an agent that trusts a wrong map
spends more than one with no map at all. A map at the level of a directory
goes out of date only when the shape of the project changes, and the signal
that it has is a run that goes looking in the wrong place.

Two smaller things help as well. Tell the agent in the ticket which files the
work touches, when you already know. And prefer a ticket that names one change
over a ticket that asks the agent to go and find out what needs changing,
because the second pays the cost of the search inside the run.

## How a run drives its agent

A run speaks the Agent Client Protocol to the database's default agent. The
agent registry stores each launch command and, where available, the command
that resumes one of its sessions.

## The agents an integration test drives

`make check` needs nothing but Go and the tools in the Makefile. The tests
behind the `integration` build tag drive a real agent, so they need that agent
installed and signed in, and they spend its tokens. The tests of
`internal/handler` skip and say which command they wanted when it is not on the
PATH; the test of `internal/cli` starts a run that has nowhere else to go, and
fails.

An agent is two commands. The ACP server is what delegator talks to, and it is
a separate npm package from the CLI of the same name; installing claude or
codex does not install it. The CLI is what a person resumes a session in, and
`codex-acp` also starts `codex` itself.

```
npm install -g @agentclientprotocol/claude-agent-acp   # claude-agent-acp
npm install -g @agentclientprotocol/codex-acp          # codex-acp
npm install -g @earendil-works/pi-coding-agent pi-acp  # pi, pi-acp
```

These are the exact commands and versions exercised by the real-agent tests:

| Agent | ACP launch argv | Terminal resume argv | Verified versions |
| --- | --- | --- | --- |
| Claude | `[claude-agent-acp]` | `[claude --resume {session}]` | claude-agent-acp 0.77.0; Claude Code 2.1.273 |
| Codex | `[codex-acp]` | `[codex resume {session}]` | codex-acp 1.11.0; Codex CLI 0.155.0 |
| Goose | `[goose acp]` | `[goose session --resume --session-id {session}]` | Goose 1.50.1 |
| OpenCode | `[opencode acp]` | `[opencode --session {session}]` | OpenCode 1.18.31 |
| GitHub Copilot | `[copilot --acp]` | `[copilot --resume={session}]` | GitHub Copilot CLI 1.0.86 |
| Cursor | `[agent acp]` | none: its terminal client does not accept an ACP session id | Cursor Agent 2026.09.15-d2fe57e |
| Pi | `[pi-acp]` | `[pi --session {session}]` | Pi 0.85.1; pi-acp 0.0.33 |

Claude, OpenCode, GitHub Copilot, and Pi add their supported one-shot flags to
the recorded resume argv. Codex and Goose have interactive-only resume
commands, so their terminal-resume subtests use a pseudo-terminal. Each
terminal assertion asks the real ACP-created session to recall text that was
never written to its repository. The `internal/cli` integration additionally
needs `claude-agent-acp`, which the run under test starts.

Run them with `make integration`, or `make release` for those and everything
`make check` does.

## Documents

- [CLI reference](https://alcubi-delegator.readthedocs.io/en/latest/)
- [Documentation publishing guide](docs/READ_THE_DOCS.md)
- [Technical document](docs/TECHNICAL_DESIGN.md)
- [Features that wait](docs/FEATURES.md)
- [Trademark policy](TRADEMARKS.md)

Alcubi Delegator is software published by Matthew McCormick. Code at
`github.com/alcubie/delegator`.
