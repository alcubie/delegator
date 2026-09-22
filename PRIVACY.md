# Privacy notice

**Effective date: September 22, 2026**

Alcubi Delegator is software published and operated by **Matthew McCormick**,
an individual. Alcubi is a brand name used for the project; this notice does
not describe a separate company.

Questions and requests about this notice may be sent to
[support@alcubi.ai](mailto:support@alcubi.ai).

## The short version

Delegator is local-first. The `dg` binary has no account system, analytics,
advertising, telemetry, crash uploader, automatic update check, or first-party
network service. It does not send ticket text, prompts, source code, logs,
repository paths, usage counts, or identifiers to Matthew McCormick.
At launch there will be no product analytics or automatic crash reporting.

Delegator does start an agent chosen by the user. A cloud-connected agent can
send data to its own operator and model provider. Delegator also lets that
agent read and change files and run tools. That third-party processing is not
collection by Matthew McCormick, but it is the main way data used with
Delegator can leave the machine. Read
[Agents and other programs](#agents-and-other-programs) before selecting an
agent.

## Data Delegator keeps locally

By default, Delegator keeps its state in
`$XDG_DATA_HOME/delegator`, `%LOCALAPPDATA%\delegator` on Windows, or
`~/.local/share/delegator` on other systems. `--data-dir` selects a different
absolute directory. Delegator creates its database, ticket files, and run logs
with permissions for the current operating-system user only. Repository files
keep their existing permissions.

The following data stays on the machine unless the user or a program the user
selected transmits it:

| Category | Local contents and retention |
| --- | --- |
| Configuration | Queue limits and timeouts, the selected default agent, and agent names, executable arguments, resume arguments, and install hints. These remain in `delegator.db` until the data directory is removed. Do not put credentials in command arguments. |
| Tickets | Ticket IDs, titles, prose, status and ordering, dependency links, state-change times, project association, branch name, session ID, and completed commit hash. Prose is in `tickets/<id>.md`; the other fields are in `delegator.db`. They are not automatically expired. |
| Repository information | Absolute project paths, default branch names, and first-commit hashes are in the database. Per-ticket Git worktrees and project cache directories are below the data directory. An accepted ticket's worktree is removed; cancelled or failed worktrees, project caches, Git branches, and commits are not automatically removed. |
| Run records | Run IDs, ticket and agent associations, supervisor process IDs, start and end times, and exit codes are in the database and are not automatically expired. |
| Usage information | If an agent reports ACP usage, Delegator stores only its aggregate input, cache-write, cache-read, output, thought, and total token counts for the run. Missing counts remain missing. Delegator does not send these counts anywhere. |
| Logs | Each run has a local JSON-lines log containing the agent's text and thought updates, tool names and summaries (which can contain commands or file paths), tool status, permission decisions, results, errors, session ID, and the agent process's standard error. Logs remain until the user removes them. |
| Prompts | Ticket prose remains in its ticket file. Delegator sends the selected agent a fixed work prompt containing the ticket ID and the absolute data, worktree, and cache paths. The agent is instructed to read the ticket locally with `dg show`. Delegator does not separately save that fixed prompt, but the agent may save or transmit its session. |
| Credentials and user identity | Delegator does not request an account, name, email address, machine ID, advertising ID, API key, or model credential. An agent process inherits the environment in which `dg` runs, so credentials already present there may be available to that agent. |

SQLite can create `delegator.db-wal` and `delegator.db-shm` beside the database
while it is open. A text editor can also create its own backup or swap files.
Those files remain subject to the editor's and operating system's behavior.

There is no remote Delegator copy for Matthew McCormick to delete. To delete
local data, first stop active runs, preserve any work that is still needed,
and remove the selected data directory. Git branches and commits live in the
project's own Git directory and must be managed with Git. Agent providers may
hold separate copies under their policies and controls.

## Network-data inventory

### The `dg` program

Ordinary `dg` commands make no first-party network request. The binary contains
no HTTP client, network listener, analytics client, crash reporter, or update
checker. `dg rpc` communicates over standard input and output, not a network
socket. The OpenRPC document contains a web URL identifying its schema;
outputting that URL does not fetch it.

Delegator runs local Git commands to inspect repositories, create and remove
worktrees, and verify commits. It does not run `git clone`, `fetch`, `pull`, or
`push`. A Git hook or helper configured in the user's repository can run any
command, including a network command; the user controls that Git configuration.

### Agents and other programs

No agent is selected by default. When the user selects one and starts a run or
uses `dg chat`, Delegator starts that executable locally and speaks ACP over
the process's standard input and output. Delegator configures no MCP servers.
It provides the agent:

- the fixed work prompt described above;
- the worktree and project-cache paths as ACP workspace roots;
- read and write access requested through ACP for absolute paths;
- approval for the agent's tool requests during an unattended run; and
- the environment inherited from `dg`, plus the project-cache variable.

The agent can read the ticket, source code, repository history, and other files;
run commands; use credentials in its environment or credential stores; and
send inputs, file contents, prompts, tool results, paths, identifiers, and
usage metadata to its operator, a model provider, an integration, or another
service. The exact recipient, purpose, retention, and controls depend on the
chosen agent, account, model, settings, and tools. Delegator and Matthew
McCormick do not control that processing.

An agent may also keep its own sessions, history, logs, or caches outside the
Delegator data directory. `dg chat` gives the terminal directly to that agent,
so Delegator does not add the interactive text to its own database or logs.
Use the agent's controls to inspect, disable, or delete its copies.

Before selecting an agent, consult its current documentation, privacy terms,
and account controls. Built-in registry entries include products associated
with [Anthropic](https://www.anthropic.com/legal/privacy),
[OpenAI](https://openai.com/policies/privacy-policy/),
[Google](https://policies.google.com/privacy),
[GitHub](https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement),
and [Cursor](https://cursor.com/privacy). Goose, OpenCode, Pi, and custom ACP
commands can use a provider selected separately by the user; consult both the
client's and the provider's terms. These links are for reference, not a claim
that Matthew McCormick speaks for or controls those services.

To avoid third-party transmission, do not start a cloud-connected agent. Use a
locally operating agent whose behavior you have verified, remove unneeded
credentials from its environment, and review the agent's own network and data
settings. Do not put secrets in tickets. Delegator has no switch that can
override or audit every network request made by a separate executable.

The editor selected with `$EDITOR` is also a separate program. It receives a
temporary file containing the ticket being edited and may have its own sync or
extension behavior. The file is removed when the editor closes, subject to the
editor's own backup and recovery features.

### Repository development and integrations

Running the compiled `dg` binary does not contact GitHub or a package host.
Cloning, pulling, opening an issue or pull request, or downloading source on
GitHub is a separate user action governed by GitHub's privacy statement.
Public issue and pull-request content, account identity, timestamps, commits,
and review history are visible to Matthew McCormick, GitHub, and the public and
may remain in Git history or copies even after an edit or deletion.

Building or testing from source can cause the Go tool to download modules from
the proxy, checksum database, or module sources selected by the user's Go
environment. The documented `npm install` commands download optional agent
clients from the user's configured npm registry. Those tools can send IP
address, user agent, requested package, version, time, account or authentication
data, and other protocol metadata to their configured services. The user can
inspect and change the Go or npm registry configuration or use an existing
offline cache. See the [Go privacy policy](https://go.dev/privacy) and
[npm privacy policy](https://docs.npmjs.com/policies/privacy/).

Tests with the `integration` build tag, including `make integration` and
`make release`, deliberately start installed, authenticated agents and submit
synthetic test prompts and repository content to them. Ordinary `go test` and
`make check` do not run those integration tests.

## Installer, releases, and updates

As of the effective date, Delegator is in development, has no published
release, and the documented `alcubi.ai` installer hostname does not resolve.
There is therefore no active Alcubi installer or update service receiving
requests. The `dg` binary performs no automatic or explicit update check.

If the installer is activated, running the documented `curl` command will be
an explicit HTTPS download. Any such service necessarily receives the source
IP address and request metadata such as time, requested path, HTTP method and
version, response status and byte count, TLS connection details, and headers
sent by the client, including its user agent and any referrer. Before that
service is activated, this notice must be updated to name its operator and
service providers, purposes, retention periods, and user controls. Re-running
an installer to replace a version will be an explicit download, not a request
made by `dg` in the background.

Official source and binary release packages must include this notice so it can
be read before or without installation.

## Email and support

Email to the privacy contact is voluntary and is the current support channel.
Suspected vulnerabilities should follow the [security policy](SECURITY.md),
which uses the same private mailbox and explains what not to send publicly.
Matthew McCormick receives the sender and recipient addresses, display names,
date and time, subject, message body, attachments, message identifiers, and
routing and delivery headers supplied by the email systems. These data are
used only to answer the request, diagnose the reported problem, keep a record
of the exchange, and address abuse, security, or legal issues.

The recipients are Matthew McCormick and the email providers used by the
sender and recipient; the recipient mailbox is provided by Google. See
[Google's privacy policy](https://policies.google.com/privacy). Messages are
kept in the support mailbox for up to 12 months after the last communication,
then deleted, unless they are still reasonably needed for an unresolved
security issue, dispute, or legal obligation. Email providers may keep backups
or logs for their own periods under their policies.

Users control what they send. Remove secrets, credentials, private repository
content, ticket text, logs, and paths that are not needed to answer the
question. A request to delete a support conversation can be sent to the same
address; Matthew McCormick will delete the mailbox copy unless it must be kept
for an unresolved security issue, dispute, or legal obligation. A request sent
by email necessarily reveals the email data described above.

## Changes to collection

Any new first-party collection must be described in this notice before it is
enabled. It must be limited to data needed for a stated purpose, offer an
explicit user choice where appropriate, and document how to disable the
collection and delete retained data.

Product analytics must not collect ticket text, prompts, source code, secrets,
repository paths, or third-party credentials by default. Crash reporting and
analytics will remain off unless a later release both changes the software and
updates this notice first. The effective date at the top will change when this
notice changes, and the repository history will preserve earlier versions.
