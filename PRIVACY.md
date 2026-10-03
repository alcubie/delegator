# Privacy notice

**Effective date: October 3, 2026**

Alcubi Delegator is software published and operated by **Matthew McCormick**,
an individual. Alcubi is a brand name used for the project; this notice does
not describe a separate company.

Questions, privacy requests, and deletion requests may be sent to
[support@alcubi.ai](mailto:support@alcubi.ai).

## The short version

Delegator is local-first and has no account system, advertising, crash uploader,
or automatic update check. It offers optional telemetry to help improve the
product. Telemetry remains off until a person explicitly submits Yes during
interactive setup or sets `telemetry` to `true`. An unanswered choice (`null`)
and No (`false`) send nothing.

With telemetry enabled, Delegator sends an installation report and aggregate
daily counts, current settings, normalized agent families, release and operating
system information, and stable identifiers. It does not send ticket text or
titles, prompts, source code, paths, project names, logs, errors, command
arguments, environment variables, credentials, session or commit IDs, or
individual ticket and run IDs.

Delegator also starts an agent chosen by the user. A cloud-connected agent can
send data to its own operator and model provider. Delegator lets that agent read
and change files and run tools. That third-party processing is separate from
Delegator telemetry and is the main other way data used with Delegator can leave
the machine. Read [Agents and other programs](#agents-and-other-programs) before
selecting an agent.

## Optional telemetry

### Consent and controls

Each Delegator data directory makes its own choice. New and migrated databases
start with `telemetry` set to `null`, meaning no answer has been submitted and no
reports may be sent. Interactive `dg init` asks only while the value is `null`.
Yes is selected in the prompt, but showing the prompt grants no permission;
pressing Enter or submitting Yes records consent. Skipping, cancellation, EOF,
or leaving setup before submission keeps `null`. Scripted setup leaves the value
unchanged unless `dg init --telemetry=true|false` is supplied.

The current value and controls are:

```sh
dg config get telemetry
dg config set telemetry true
dg config set telemetry false
```

`dg config get telemetry` returns `true`, `false`, or `null`. Setting `false`
stops new reporting but does not contact the collector or delete reports already
received. Setting `true` after `false` begins a new consent period without
backfilling the disabled period. Copying a data directory also copies its choice
and instance identity; Delegator does not scan for or detect copied databases.
There is no environment override, public report preview, telemetry command group,
or self-service deletion command.

### What is sent

Reports are HTTPS POST requests to `https://telemetry.alcubi.ai/v1/reports`.
Every request has a format marker, the database's random instance UUID, and one
or more reports with a deterministic report UUID, kind, and UTC date.

The first submitted Yes causes an immediate installation-report attempt. The
report contains the date of first consent, Delegator release version, operating
system family, consent-notice version, and an optional derived machine identifier.
It measures consenting initialized instances, not downloads, installations,
unique devices, or people.

Daily reports contain aggregate counts for:

- tickets created, first becoming ready, accepted, and cancelled;
- runs started, ended, and ended unsuccessfully; and
- runs started by an allowlisted agent family, with unrecognized or modified
  agents reported as `custom`.

They also contain the current `runs`, `max_runs_per_project`, `timeout_minutes`,
`done_hours`, and normalized default-agent setting. Settings are a snapshot when
the report is built; they are not the historical settings for that day. Custom
agent names, executable paths, arguments, install hints, and ACP metadata stay
local.

The instance UUID is generated once in SQLite. Each installation or daily report
ID is a deterministic UUID derived from that instance UUID, so retries can be
recognized. If available, Delegator also derives an application-specific
HMAC-SHA256 machine hash after consent from an operating-system machine identifier,
or from one deterministically selected network-interface address as a fallback.
It never sends the raw machine identifier or address. The application key is a
public domain separator, not a secret. A machine hash may change or be duplicated,
and a hash derived from a hardware address may be guessable. These stable values
are pseudonymous identifiers that can link reports from the same instance or
machine over time.

### Reporting schedule and failures

Delegator reports only whole UTC days covered entirely by the current uninterrupted
consent period. It skips the partial day when telemetry is enabled, every disabled
or interrupted day, and the current unfinished day. When Delegator next runs, it
catches up every eligible day through yesterday, including zero-count days, within
the most recent 90 days. A zero-count row confirms reporting coverage and is not
evidence of active use.

The installation request runs synchronously after consent. Later ordinary commands
and run completion can start a short-lived sender; there is no permanent telemetry
daemon. Failed requests are rebuilt from local data and may retry after at least
15 minutes. A server can require a longer delay. Requests have bounded timeouts,
do not follow redirects, and never change a command's output, exit status, ticket
state, or queue progress. A request already in flight may arrive after telemetry
is disabled.

### Collector, providers, and retention

The collector runs on Cloudflare Workers and stores reports in Cloudflare D1.
It validates recognized fields and constructs a new allowlisted object at every
level, discarding unknown fields before storage. Accepted reports are stored as a
small indexed envelope plus sanitized JSON. The collector inserts each report ID
once; a retry succeeds without replacing the first stored receipt, counts, or
settings. It commits the entire accepted batch before acknowledging it.

The application does not store source IP addresses, request headers, raw request
bodies, credentials, or request-specific error details in its telemetry tables or
logs. Cloudflare necessarily receives network data such as the source IP address,
request time, requested host and path, protocol details, and user agent while
delivering and protecting the service. Cloudflare processes that data under its
[privacy policy](https://www.cloudflare.com/privacypolicy/). Telemetry reports are
kept separate from website visits and support email and are not used for billing,
authorization, advertising, or profiling individuals.

Linkable installation, usage, and settings reports are removed after 90 days,
based on report date or receipt time. Cloudflare D1 recoverable history can retain
deleted data separately for up to 30 days, depending on the service plan. Restores
must reapply deletion records before ingestion resumes. Delegator does not keep
longer-lived identified telemetry aggregates.

To request deletion of received telemetry, email
[support@alcubi.ai](mailto:support@alcubi.ai). Locating the records may require the
instance UUID stored in the local `telemetry_state` database table. The maintainer
uses protected Cloudflare access to add that UUID to a revocation register, which
atomically deletes its installation, usage, and settings reports. The UUID and
revocation time remain for the life of the service solely to reject delayed or
re-enabled reports; no machine hash is kept in that register. This minimal record
is an exception to the 90-day report retention period. Disabling telemetry alone
does not make this deletion request.

## Data Delegator keeps locally

By default, Delegator keeps its state in `$XDG_DATA_HOME/delegator`,
`%LOCALAPPDATA%\delegator` on Windows, or `~/.local/share/delegator` on other
systems. `--data-dir` selects a different absolute directory. Delegator creates
its database, ticket files, and run logs with permissions for the current
operating-system user only. Repository files keep their existing permissions.

| Category | Local contents and retention |
| --- | --- |
| Configuration and telemetry state | Queue limits and timeouts, default agent and model, agent names and commands, the nullable telemetry choice, instance UUID, consent dates, delivery cursor, and attempt state. These remain in `delegator.db` until the data directory is removed. Do not put credentials in command arguments. |
| Tickets | Ticket IDs, titles, prose, status and ordering, dependency links, state-change times, project association, branch name, session ID, and completed commit hash. Prose is in `tickets/<id>.md`; the other fields are in `delegator.db`. They are not automatically expired. |
| Repository information | Absolute project paths, default branch names, and first-commit hashes are in the database. Per-ticket Git worktrees and project cache directories are below the data directory. An accepted ticket's worktree is removed; cancelled or failed worktrees, project caches, Git branches, and commits are not automatically removed. |
| Run records | Run IDs, ticket and agent associations, supervisor process IDs, start and end times, and exit codes are in the database and are not automatically expired. |
| Agent-reported usage | If an agent reports ACP usage, Delegator stores aggregate input, cache-write, cache-read, output, thought, and total token counts for the run. Missing counts remain missing. These token counts are not included in telemetry. |
| Logs | Each run has a local JSON-lines log containing the agent's text and thought updates, tool names and summaries, tool status, permission decisions, results, errors, session ID, and standard error. Logs remain until the user removes them. |
| Prompts | Ticket prose remains in its ticket file. Delegator sends the selected agent a fixed work prompt containing the ticket ID and absolute data, worktree, and cache paths. The agent may save or transmit its session. |

SQLite can create `delegator.db-wal` and `delegator.db-shm` beside the database
while it is open. A text editor can also create backup or swap files subject to
the editor's and operating system's behavior.

To delete local data, first stop active runs, preserve needed work, and remove the
selected data directory. Git branches and commits live in the project's Git
directory and must be managed with Git. Removing local data does not delete
telemetry already received or copies held by agent providers.

## Other network-data processing

### Agents and other programs

No agent is selected by default. When the user selects one and starts a run or
uses `dg chat`, Delegator starts that executable locally and speaks ACP over the
process's standard input and output. Delegator configures no MCP servers. It gives
the agent the work prompt, workspace paths, approved tool access during unattended
runs, and the environment inherited from `dg`.

The agent can read repository and ticket data, run commands, use available
credentials, and send data to its operator, model provider, integrations, or other
services. The recipients, purposes, retention, and controls depend on the chosen
agent, account, model, settings, and tools. Consult their current documentation
and privacy terms. Built-in entries include products associated with
[Anthropic](https://www.anthropic.com/legal/privacy),
[OpenAI](https://openai.com/policies/privacy-policy/),
[Google](https://policies.google.com/privacy),
[GitHub](https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement),
and [Cursor](https://cursor.com/privacy). Goose, OpenCode, Pi, and custom ACP
commands can use separately selected providers.

To avoid third-party transmission, use a locally operating agent whose behavior
you have verified, remove unneeded credentials from its environment, and do not
put secrets in tickets. The telemetry setting cannot control requests made by a
separate executable. The editor selected with `$EDITOR` is also a separate program
and may have its own sync or extension behavior.

### Repository development and integrations

Cloning, pulling, opening an issue or pull request, or downloading source on
GitHub is governed by GitHub's privacy statement. Building or testing from source
can cause Go or npm tooling to contact configured package services. Tests with the
`integration` build tag, including `make integration` and `make release`, start
installed, authenticated agents and submit synthetic prompts and repository
content. Ordinary `go test` and `make check` do not run those paid integration
tests.

### Installer, releases, and updates

The public installer URLs are active. A request to `alcubi.ai` first reaches
Cloudflare and redirects to a release asset hosted by GitHub. Cloudflare, GitHub,
and their delivery providers receive the source IP address and ordinary HTTPS
request metadata under their respective privacy policies. Running an installer
again is an explicit download. The `dg` binary performs no automatic or explicit
update check.

Official source and binary release packages include this notice so it can be read
before or without installation.

### Email and support

Email to the privacy contact is voluntary. Matthew McCormick and the providers
used by the sender and recipient receive the message and routing data. The
recipient mailbox is provided by Fastmail; see
[Fastmail's privacy policy](https://www.fastmail.com/about/privacy/). Messages are
kept for up to 12 months after the last communication, then deleted unless needed
for an unresolved security issue, dispute, abuse response, or legal obligation.
Email providers may keep backups or logs for their own periods.

Remove secrets, credentials, private repository content, ticket text, logs, and
paths that are not needed. A request to delete a support conversation can be sent
to the same address; the mailbox copy will be deleted unless retention is required
for one of the reasons above. Sending a request necessarily reveals the email and
routing data needed to receive and answer it.

## Changes to this notice

New first-party collection will be described here before it is enabled and will
be limited to a stated purpose. Delegator telemetry will continue to exclude
ticket text, prompts, source code, secrets, repository paths, logs, and third-party
credentials. The effective date changes with this notice, and repository history
preserves earlier versions.
