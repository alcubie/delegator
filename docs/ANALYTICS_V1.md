# Optional analytics: JSON reports

Ticket 298 defines examples for the September 30, 2026 design. This change does
not implement a sender, consent UI, collector, or production service.

## Small envelope, flexible data

POST JSON to `/v1/reports` on the planned `https://telemetry.alcubi.ai` collector.
The request contains `format: 1`, the database's `instance_id`, and a `reports`
array. Each entry has `id` (report UUID), `kind` (`installation` or `daily`),
`date` (`YYYY-MM-DD`, UTC), and `data` (the approved metrics/metadata object).
Examples live in `testdata/analytics/v1/`. Copy these and this note into the web
service with the source commit recorded; ordinary builds stay offline.

D1 stores a few indexed envelope columns and the sanitized `data` as JSON text.
There is no formal JSON Schema, custom schema validator, metric-column migration,
or exact response-echo protocol. Additive optional metrics keep format 1. Change
format only for incompatible envelope or meaning changes; never silently change
the meaning of an existing counter. Missing historical metrics mean uncollected,
not zero. Actual client/collector tests own validation of their implementations.

## IDs and INSERT-only storage

Use UUID v5 with the parsed instance UUID as the namespace and these UTF-8 names:

| Kind | UUID name | Date |
| --- | --- | --- |
| Installation | `installation` | First affirmative consent's UTC date |
| Daily | `daily:YYYY-MM-DD` | The day being counted |

UUID v5 is used for repeatable report identity, not authentication or machine
identification. It is independent of payload, batch, retry time, format marker,
and consent period. A report ID is per entry, never just per HTTP request.
Persist the first consent date for installation retries. The instance UUID stays
the same across upgrades and re-enablement; no moved-database detection.

A reports table needs `report_id` as a unique key, `instance_id`, `kind`, `date`,
server `received_at`, and JSON `data`. Use targeted
`ON CONFLICT(report_id) DO NOTHING`; other database errors must not be swallowed.
Validate that IDs match their instance/kind/date derivation. A duplicate is a
successful no-op, even if its otherwise valid payload differs: first receipt
wins. Do not replace settings, counters, metadata, or receipt time on replay.
Derive settings views from stored reports instead of maintaining an UPDATE-based
latest-settings table. Expiry/operator deletion can DELETE; ingestion only INSERTs.

For installation and daily entries together, sanitize and validate the entire
request, insert all entries in one transaction, commit, then return HTTP 204.
Duplicates also get 204. On failure, acknowledge none; do not partially advance
client state. A lost response is safe to retry. The client needs no JSON response
body or echoed timestamps, only 204 from the configured HTTPS endpoint. It must
not follow redirects and send reports to a different destination.

## Consent, catch-up, and retries

NULL/false consent sends nothing. A submitted Yes permits an immediate
installation attempt; this measures consent during init, not binary downloads.
An unanswered Yes-selected prompt is not consent. Re-enabling does not create a
new installation ID or send disabled-period history.

Daily counts cover whole UTC days only. On a due invocation, capture today's UTC
midnight as the exclusive cutoff and query all eligible days from the successful
cursor to that cutoff. The first eligible day is the first UTC midnight at or
after the current uninterrupted consent start. Thus midday opt-in skips that
partial day; disabling and re-enabling skips the interrupted day. Setting true
when already true does not restart consent. No partial-day/consent-period rows.

Catch up missed days in one request, including zero-count days within eligible
coverage; those are not evidence of active use. Keep the agreed 90-day retention
bound and skip older unreported days. Never include the current incomplete day.
A pending installation can join the batch. If there are no eligible days or
pending installation, there is nothing to send.

After 204, advance the local cursor to the captured cutoff and mark installation
acknowledged if it was included, only if consent is still true and unchanged.
Do not advance to response time. Until another UTC day completes there is no
new daily report, even when dg runs often. Local settings/cursor writes may UPDATE;
the INSERT-only restriction is for server report ingestion.

After a failure, keep the cursor and rebuild later, waiting at least 15 minutes
between attempts. Atomically reserve the attempt time in SQLite so simultaneous
commands do not send together; consent changes must not bypass a pending cooldown.
Later dg invocations or supervisor completions trigger retries, not a sleep loop
or permanent daemon. Honor longer server retry delays. A retry after midnight
may contain more days, while previous entries retain their UUIDs. Reread consent
before HTTP and condition local acknowledgement on the same consent period.

No outbox, saved payload, event stream, final-day flush, or immediate retry loop.
Use bounded network timeouts outside SQLite transactions. Analytics failure must
not fail or change output of the user's command. Significant clock changes and
late changes to old local history are accepted best-effort limitations, not a
reason to overwrite already received daily reports.

## Data collected

Installation data contains available derived machine hash, normalized product
version, OS family, and consent-notice version. Daily data adds these counts:

| Metric | Meaning within the whole UTC day |
| --- | --- |
| `tickets_created` | Creation transitions, once per ticket |
| `tickets_first_ready` | Tickets first entering ready; find first-ever ready before filtering by day |
| `tickets_accepted` | Transitions to done, including forced acceptance |
| `tickets_cancelled` | Transitions to cancelled |
| `runs_started` | Runs whose start is in the day |
| `runs_ended` | Runs whose non-null end is in the day |
| `runs_ended_unsuccessfully` | Ended runs with nonzero or missing exit status |
| `runs_started_by_agent` | Starts by known agent family; unrecognized/custom agents become `custom` |

Repeated commands without a committed transition count nothing. A run can start
and end on different days; exit zero is not proof of ticket readiness/acceptance.
Report known zero counts as zero. Agent entries with zero starts may be omitted.

Daily `settings` contains only `runs`, `max_runs_per_project`, `timeout_minutes`,
`done_hours`, and normalized `default_agent` (null if unset). Metadata/settings
are observed when building the report, not reconstructed for each historical day.
They are kept from the first received entry, even if a retry observes new settings.
Use clean release versions, otherwise `development`, without branches or hashes.

## Privacy and minimal validation

Clients construct typed payloads from explicit fields. The collector constructs a
new allowlisted object before storing JSON; discard unknown fields at every level
rather than rejecting the whole report or storing raw request bytes. Do not log
unknown values. New fields need a reviewed collector allowlist before clients send
them if their retention matters; older collectors can drop them without breaking
existing reports. No general arbitrary-property or free-text bag is permitted.

Known fields still need simple checks: valid JSON/envelope/UUIDs/dates, a bounded
batch/body (up to 91 entries / 128 KiB for installation plus 90 days), nonnegative
safe-integer counters, supported kinds/format, completed unexpired daily dates,
and bounded/normalized metadata/settings. Missing optional metrics are allowed;
malformed recognized values reject the whole batch. No mandatory fractional
timestamp precision, full agent zero-vector, or schema version bump for additions.

Agent families are `claude`, `codex`, `gemini`, `opencode`, `goose`,
`github-copilot`, `cursor`, `pi`, and `custom`. Client normalization must establish
a known built-in configuration locally, not trust a user-editable registry name
alone. Unknown or modified entries fall back to `custom`. The collector drops
unrecognized family keys. No registry names, argv, executable paths, prompts,
ticket text, code, logs, project/commit/session IDs, or raw machine identifiers.
Machine hash may be omitted/null when unavailable; CPU architecture is excluded.

Return a non-204 status for rejected requests or failures; never echo private
input. A revoked instance receives 410 and must stop automatic attempts without
silently rotating identity. Operator deletion/retention remains separate from
the client protocol: no public deletion route, credential, or analytics command.
Retain minimal revoked-ID suppression separately from expiring usage so delayed
installation retries cannot recreate deleted data. Document that retention.

## Examples and checks

`installation.json` is sent immediately after consent on September 25 at noon.
`catch-up.json` covers September 26 and 27 (including an idle day).
`retry-expanded.json` repeats those IDs plus September 28 after a lost reply.
The partial consent day (25) and current day (29) are not reported.

Run `make analytics-contract-check` for the small example check: parse JSON,
verify UUID derivation, and replay it into an in-memory SQLite table with a unique
report ID. This is not a schema validator or substitute for production tests.
Downstream tests must cover consent, day/cursor boundaries, 15-minute retry
cooldowns, concurrent senders, sensitive-field stripping, and atomic failure.
