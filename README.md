# delegator

Delegate long tasks to a coding agent, then come back to one ordered inbox.

Delegator does one thing: it controls the queue and the isolation of the work. It does not
write code, do a diff, or examine the work. Those tasks belong to programs that you
select.

Status: in development. Nothing here is released yet.

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
```

| Test | Needs |
| --- | --- |
| `internal/handler` claude | `claude-agent-acp` and `claude` |
| `internal/handler` codex | `codex-acp` and `codex` |
| `internal/handler` opencode | `opencode`, signed in with `opencode auth login` |
| `internal/handler` GitHub Copilot | `copilot` 1.0.85 or later |
| `internal/cli` | `claude-agent-acp`, which the run under test starts |

Run them with `make integration`, or `make release` for those and everything
`make check` does.

## Documents

- [Technical document](docs/TECHNICAL_DESIGN.md)
- [Features that wait](docs/FEATURES.md)

An Alcubi product. Code at `github.com/alcubie/delegator`.
