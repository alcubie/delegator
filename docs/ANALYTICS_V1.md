# Optional analytics report contract v1

Status: implementation contract for ticket 298, not an enabled service. This
implements step 1 of the approved September 29, 2026 analytics design. No sender,
consent UI, infrastructure or public policy changes ship with this contract.

## Canonical artifacts

This repository owns `docs/analytics/v1/request.schema.json`,
`response.schema.json` in the same directory, and `testdata/analytics/v1/`.
The schemas use JSON Schema draft 2020-12, with no external references. The
rules below additionally constrain relationships, time and transport. Both Go
and Worker implementations must enforce them; schema validation alone is not
sufficient. Unknown properties are forbidden at every object level.

Run `make analytics-contract-check` (Python 3 standard library only) to check
the committed fixtures, rejection cases, acknowledgements and upsert scenarios.
The checker implements only the schema keywords used here, fails on unsupported
keywords, and is a fixture check, not a production validator or collector.

The web collector ticket must copy both schemas, this document and the complete
fixture directory into its repository, recording the source Git commit in its
import manifest. Commit those copies; ordinary Go/Worker/site builds must never
fetch this repository or require a sibling checkout. Coordinate incompatible
changes through a new protocol version; do not silently expand the v1 allowlists.

## Transport and serialization

POST `/v1/reports` over HTTPS; the planned production origin is
`https://telemetry.alcubi.ai`. Tests use local services only. No authentication or
deletion credential is embedded in the CLI. Identifiers do not prove authenticity.
Accept `application/json`, optionally `charset=utf-8`, with UTF-8 JSON and no
content encoding. Limit the actual request body to 131072 bytes, including
whitespace; do not trust Content-Length. Reject duplicate JSON property names,
invalid UTF-8, non-JSON numbers, trailing data and a non-object root.

All schema properties are required, including explicit nulls. Integers must be
finite integral JSON numbers (no strings or booleans); writers use decimal integer
notation. Counts and numeric settings are bounded by 2147483647. Counters are
exact, never clamped: if aggregation exceeds the bound, skip the attempt without
advancing the cursor. For larger valid local settings, clamp only their reported
snapshot to this bound. Object property order and whitespace have no meaning.

All timestamps use UTC `YYYY-MM-DDTHH:mm:ss.fffffffffZ` with exactly nine fractional
digits, valid Gregorian dates, years 0001–9999 and seconds 00–59. No offsets or leap
seconds. Dates are `YYYY-MM-DD`. Nanoseconds distinguish consent periods even if
the database's existing history has coarser timestamps; preserve that history's
actual precision when comparing instants. Implementations must not truncate
identity/acknowledgement timestamps through JavaScript Date millisecond conversion.

## Request fields

| Field | Meaning |
| --- | --- |
| `schema_version` | Integer 1; path and body version must agree |
| `consent_notice_version` | Integer 1, identifying the submitted notice; independent of release version |
| `instance_id` | Lowercase canonical random UUID v4 persisted in this database |
| `machine_id` | 64 lowercase hex characters from the application-specific hash, or null if unavailable; never a raw OS identifier or MAC |
| `consent_started_at` | Start of the current uninterrupted permitted period |
| `generated_at` | UTC time at which this request and its current metadata/settings snapshot were captured |
| `version` | Normalized Delegator release, or `development` |
| `os` | `linux`, `darwin`, `windows`, or `other`; never CPU architecture |
| `installation` | `{ "first_consent_at": timestamp }` if installation acknowledgement is pending, otherwise null |
| `daily` | Coverage, nonempty daily rows and current settings, or null |

At least one of `installation` and `daily` is non-null. Machine ID is nullable,
not omitted, and may become available/change later. Its derivation is client
implementation work; it is best-effort grouping, not an authentication mechanism.
Do not generate a replacement random machine ID on failure.

Persist `first_consent_at` once, across all later consent changes. It is no later
than `consent_started_at`, which is no later than `generated_at`. NULL/false
consent prohibits any request. NULL/false-to-true creates a new consent start;
true-to-true preserves it. To avoid a duplicate period after a clock reversal or
rapid toggle, allocate a start strictly greater than the previous start (at least
one nanosecond); wait until wall time catches up before constructing a request.
Do not rotate the instance UUID on re-enable or copy detection.

Normalize an exact clean stable release tag `vMAJOR.MINOR.PATCH` by removing `v`.
Each numeric component has no leading zero except zero and at most nine digits.
All other builds, including prerelease tags, build metadata, dirty trees and
untagged Git descriptions, become `development`; never send branch names or
hashes. OS is the runtime OS mapped through the fixed enum. Both are current at
generation time, not reconstructed per usage day.

## Daily coverage and counting

`daily` contains `from`, `through`, `days`, and `settings`. Coverage is the
half-open instant interval `[from, through)`. Capture `through` as the start of
the current UTC day at generation time, not the response time. Set `from` to the
latest of current consent start, successful cutoff, and `through` minus 90 UTC
days. Discard older unreported analytics without changing local ticket history.
If `from >= through`, there is no completed range to upload; never emit a
negative range or move a cursor backwards after a clock change.

`days` has 1–90 rows, sorted by increasing unique date. Each date intersects
coverage and is strictly before the date of `through`. The first date may be
partial; count only timestamps within coverage. Omit rows whose seven counters
are all zero. Omitted dates in coverage mean zero activity, not missing pages.
Never split a bounded catch-up request into multiple uploads on the same day.

For an entirely empty range, send no daily report or settings heartbeat. The
client may advance its local cutoff through that empty range, conditional on
unchanged consent and the same consistent history snapshot. If installation is
pending, send installation alone. Count only committed history visible in the
snapshot; late history edits/deletions and significant clock changes are accepted
best-effort limitations, not reasons to invent events or negative counts.

| Daily counter | Exact rule |
| --- | --- |
| `tickets_created` | Committed creation transitions (`from_status` NULL), one per ticket |
| `tickets_first_ready` | Each ticket's first-ever transition into `ready`; find it over all available history **before** filtering coverage |
| `tickets_accepted` | Committed transitions into `done` (including forced acceptance) |
| `tickets_cancelled` | Committed transitions into `cancelled` |
| `runs_started` | Run rows whose `started_at` falls in coverage |
| `runs_ended` | Run rows with non-null `ended_at` falling in coverage |
| `runs_ended_unsuccessfully` | Those ended rows with a nonzero or missing exit code |

Repeated commands with no committed transition count nothing. Accepted/cancelled
count transitions rather than current ticket status; first ready counts each
ticket at most once in its lifetime. Legacy rows with no creation or
ready transition are not inferred from current status. Assign events exactly at
midnight to the following UTC day. A run spanning midnight contributes a start
and an end on different days; an end can count even when its start predates
consent. An unfinished run is not an unsuccessful end. Exit zero says only that
execution succeeded; it says nothing about ticket readiness, acceptance or merge.
Unsuccessful ends cannot exceed ends; starts need not equal ends on a given day.

`runs_started_by_agent` has every family below as a required integer, including
zeros. Its sum equals `runs_started`. The family is determined from the available
local registry entry when aggregating (historical command reconstruction is not
available); missing/deleted/unrecognized entries become `custom`.

| Family | Exact built-in registry name and launch argv required |
| --- | --- |
| `claude` | `claude`, `["claude-agent-acp"]` |
| `codex` | `codex`, `["codex-acp"]` |
| `gemini` | `gemini`, `["gemini", "--experimental-acp"]` |
| `opencode` | `opencode`, `["opencode", "acp"]` |
| `goose` | `goose`, `["goose", "acp"]` |
| `github-copilot` | `github-copilot`, `["copilot", "--acp"]` |
| `cursor` | `cursor`, `["agent", "acp"]` |
| `pi` | `pi`, `["pi-acp"]` |
| `custom` | Everything else, including a familiar name with modified argv |

Compare exact case-sensitive strings/argument arrays locally; do not trust a
registry name alone, resolve paths, inspect executable contents, infer a family
from a basename or transmit argv. Resume commands and install hints do not affect
the launch family. An alias with a different name is conservatively `custom`.

`settings` contains `runs` and `timeout_minutes` (positive integers),
`max_runs_per_project` and `done_hours` (nonnegative integers), and `default_agent`
(a family above, or null when no default is configured). Zero retains its local
meaning: no separate project cap, or no DONE display window. Unknown/custom
default agents become `custom`. These are current settings at `generated_at`,
never historical settings for each day. Use typed fields, not config serialization.

## Persistence, retries and acknowledgement

The collector validates the complete request before any writes and commits all
installation, daily and latest-snapshot writes atomically before returning HTTP
200. Installation records deduplicate by `instance_id`; retries/re-enables never
add an installation. Preserve the earliest `first_consent_at` seen for that UUID.
Daily rows upsert by `(instance_id, consent_started_at, date)`, replacing totals,
never adding to them. Omitted days do not delete existing rows. Two permitted
portions of a day therefore remain separate rows whose totals can be summed.
Daily-only reports remain valid even if installation has expired under retention.

Use `generated_at` to order current metadata/settings snapshots per instance.
An older request may upsert its historical daily rows but must not replace newer
metadata or settings. Equal timestamps keep the stored snapshot. Installation-only
requests do not erase settings. Likewise, an older snapshot must not replace a
daily row written by a newer generation; equal generations are idempotent (keep
the existing row). Store generation ordering with the rows, not just arrival time.

The exact success response has `schema_version: 1`, `status: "accepted"`, the
request's `instance_id`, `consent_started_at`, `generated_at`,
`installation_acknowledged` (true exactly when installation was non-null), and
`reported_through` (the requested daily `through`, or null).
There is no partial acknowledgement. A duplicate succeeds with the same logical
acknowledgement even when every write was already applied or superseded.

Only HTTP 200 with a valid success schema and **all** matching echoes acknowledges
the request. A malformed, mismatched, truncated, empty or unknown-field response,
204, timeout, lost response or any other status is failure: do not mark installation
acknowledged or advance usage. Limit a client-read response to 4096 bytes. After
success, change local state only if consent is still true and its start is still
the request's start. Advance the cursor to captured `through`, never response time,
and never backwards. An installation-only acknowledgement cannot advance usage.

Attempt installation immediately after the first persisted Yes. Reserve attempts
atomically in SQLite and leave no transaction open during HTTP. Subsequently
wait at least 24 hours between attempts (including failures); consent toggling
does not bypass an existing attempt reservation. Bundle pending installation with
due daily usage. Retries rebuild from history and may cover additional completed
days. Recheck consent before the request; an already in-flight request can arrive
after disable. No outbox, background daemon or final-day flush is introduced.

## Rejections, retention and deletion

Error JSON is exactly `{ "schema_version": 1, "error": code }`; do not echo
invalid fields, payloads or arbitrary validation messages. Supported pairs:

| HTTP | `error` | Client effect |
| --- | --- | --- |
| 400 | `invalid_json` | No acknowledgement; retry only when next due |
| 413 | `request_too_large` | Same |
| 415 | `unsupported_media_type` | Same |
| 422 | `unsupported_version` | Same; do not fall back to another schema/notice |
| 422 | `invalid_report` | Same; includes unknown fields, invalid counts/dates |
| 410 | `identity_revoked` | Terminal rejection for this UUID; suppress future sends without changing the user's saved consent or rotating identity |
| 429 | `rate_limited` | No acknowledgement; honor a longer valid Retry-After, never shorten 24 hours |
| 503 | `unavailable` | No acknowledgement; retry when next due |

Schema and notice versions other than integer 1 are unsupported. Unknown fields
are rejected even if apparently harmless. Validate structure before checking a
revocation record. Any revoked UUID rejects the whole request without restoring
rows. Any unexpected HTTP/error body is an unacknowledged failure; only a validated
410 error is terminal. No client sends telemetry about rejection.

Collector time allows `generated_at` up to five minutes in the future. Reject a
daily request if any date is older than the collector's current UTC date minus
90 days; also enforce the generation-relative coverage bound. Reject the entire
request, not individual expired rows. A later due attempt recomputes its retention
floor. First consent can be older than 90 days; it is identity metadata, not usage
to backfill. Linkable installation/settings records expire 90 days after their
last accepted applicable report; daily records expire by date. Any identifier-free
coarse aggregate retention (up to 24 months) is operator policy, not this payload.

Keep transport/infrastructure rate limits separate from validation; a legitimate
duplicate must be safe even if it arrives within 24 hours. The client attempt
reservation enforces normal cadence; the unauthenticated service cannot prove it.

Operator-only deletion removes installation, daily and settings records and
maintains a protected UUID suppression record indefinitely while v1 ingestion is
enabled, since an installation-only retry has no finite age limit. Do not expire
that record with the 90-day data retention. Restores must reapply suppression
before accepting traffic. Suppression is minimal deletion bookkeeping, not usage
data. There is no public deletion route, deletion credential or CLI command.
Disabling stops sends; it does not request deletion. No ticket text, code, paths,
logs, project/agent registry names, command arguments, interaction events or CPU
architecture belong in a request.

## Fixture scenarios

`testdata/analytics/v1/cases.json` supplies collector time and expected validation
for every request/response fixture. `sequence.json` orders successful requests
and gives final row counts/totals and the latest settings timestamp. It includes
a lost-response retry expanded by one day, a delayed older settings snapshot, and
two consent periods on September 27. Reapplying the sequence changes no totals.
This is a collector upsert sequence, not a possible client consent/scheduling
timeline: its independently constructed period snapshots exercise the composite
key. With completed days and no final flush, a normal client cannot report the
first portion of a day after that period was disabled. Do not add a flush or
send under revoked local consent to reproduce this fixture sequence.
`normalization.json` is local-only input/output data (including deliberately
private agent strings), never an upload. `counting.json` supplies synthetic
transitions/runs and expected rows, covering first-ready before consent, repeated
finish without a new transition, midnight boundaries, partial consent, nonzero/missing exits, unfinished
runs and forced acceptance. These inputs are for downstream aggregation tests;
the check verifies that their expected rows match the shared wire examples.
