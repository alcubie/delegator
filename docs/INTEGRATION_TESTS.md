# Alcubi Delegator real-agent integration tests

These tests are for maintainers. They are not prerequisites for a person who
uses `dg`. They start authenticated third-party agents, can send synthetic
repository content to agent providers, and can consume paid tokens.

`make check` compiles the tests behind the `integration` build tag but does not
run them. Use `make integration` to run them. `make release` runs them and then
builds publication-free release artifacts.

## Agent commands exercised

An agent can use two commands. The ACP command is the server that Delegator
starts for unattended work. A separate terminal command can resume the saved
session for `dg chat`. Installing an agent's normal CLI does not always install
its ACP adapter.

The live tests record these commands and versions:

| Agent | ACP launch command | Terminal resume command | Verified versions |
| --- | --- | --- | --- |
| Claude | `claude-agent-acp` | `claude --resume {session}` | `claude-agent-acp` 0.77.0; Claude Code 2.1.273 |
| Codex | `codex-acp` | `codex resume {session}` | `codex-acp` 1.11.0; Codex CLI 0.155.0 |
| Goose | `goose acp` | `goose session --resume --session-id {session}` | Goose 1.50.1 |
| OpenCode | `opencode acp` | `opencode --session {session}` | OpenCode 1.18.31 |
| GitHub Copilot | `copilot --acp` | `copilot --resume={session}` | GitHub Copilot CLI 1.0.86 |
| Cursor | `agent acp` | none | Cursor Agent 2026.09.15-d2fe57e |
| Pi | `pi-acp` | `pi --session {session}` | Pi 0.85.1; `pi-acp` 0.0.33 |

Cursor's terminal client did not accept an ACP session ID in the verified
version, so its test covers ACP start and session load but not `dg chat`.
Claude, OpenCode, GitHub Copilot, and Pi add supported one-shot flags during
the terminal assertion. Codex and Goose use a pseudo-terminal because their
resume commands are interactive.

These versions are evidence of a successful test, not a compatibility floor or
support guarantee. Gemini is present in the built-in registry but has no live
integration test in this repository.

## Install the optional adapters

Only install commands that you intend to test. The ACP adapters for Claude,
Codex, and Pi are separate npm packages:

```sh
npm install -g @agentclientprotocol/claude-agent-acp
npm install -g @agentclientprotocol/codex-acp
npm install -g @earendil-works/pi-coding-agent pi-acp
```

Install and authenticate the matching terminal clients through their own
current instructions. Goose, OpenCode, GitHub Copilot CLI, and Cursor Agent
provide the ACP command in their installed client. Confirm the exact programs
that Delegator can find:

```sh
dg agents --all
```

## Run and interpret the tests

```sh
make integration
```

The runner requires Python 3. It streams test and subtest starts/results, prints
elapsed-time heartbeats every 15 seconds (including during builds), and ends
with a failure summary. Agent chatter stays in the full log; the terminal
shows only the last 12 diagnostic lines for failures, with long lines shortened.
The printed temporary directory retains `output.log` and the original Go JSON
stream in `events.jsonl` after the tests finish. Remove it when no longer needed.
Every invocation runs fresh tests instead of using Go's cached results.

To rerun one agent with the same output format:

```sh
make integration INTEGRATION_ARGS='--run ^TestIntegrationCodex ./internal/handler'
```

The handler tests create a temporary Git repository, start a real ACP session,
ask the agent to make and commit a small change, and then test session recall
where the agent supports it. The recall prompt asks for text that was in the
session but never in the repository. This checks that the terminal command
resumed the same session.

The `internal/cli` integration test additionally needs `claude-agent-acp`. It
starts a complete ticket run. If an agent command is absent, handler tests skip
and name the missing command; the CLI integration has no alternate agent and
fails.

Run live tests only with accounts, repositories, and credentials that you are
authorized to use. Review [the privacy notice](../PRIVACY.md) before sending
test content to an agent provider.
