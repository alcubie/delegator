# Optional self-upgrades

**Status:** Deferred at the user's request on 2026-10-06.
This document describes future work, not available functionality.
The full historical breakdown is in [AUTO_UPGRADE_TICKETS.md](AUTO_UPGRADE_TICKETS.md).

## Intended behavior

Discover new Delegator releases automatically during ordinary interactive use,
but install only when the user explicitly agrees.

1. Consult a per-user cache when an eligible command runs. Check GitHub Releases
   at most once every 24 hours using bounded background work; no permanent updater
   is required. Do not delay normal commands for networking. A newly discovered
   update may appear on the next invocation.
2. After a successful interactive command finishes, display installed/available
   versions and ask `Upgrade now? [y/N]`.
3. Yes invokes the shared upgrade service. Empty input, end of input, cancellation,
   and No never install. Declining snoozes the prompt for 24 hours.
4. Show download progress, verification, installation, and final version
   confirmation. The next invocation uses the new version. Never rerun the
   original command.

Illustrative terminal output:

```text
Delegator v1.3.0 is available (installed: v1.2.0).
Upgrade now? [y/N] y

Downloading v1.3.0… 100%
Verifying download… done
Installing… done
Upgraded to v1.3.0. Your next dg command will use it.
```

Provide `dg upgrade` for explicit upgrades using the same installation service.
Calling it authorizes installation and forces a fresh release check. It should
work without project initialization or opening/migrating the ticket database.
The proposed `DG_NO_UPDATE_CHECK` setting disables automatic checks and prompts,
while leaving explicit upgrades available.

## Eligibility and failures

Exclude development/snapshot builds, noninteractive and coding-agent calls,
structured output, remote command dispatch, background workers, help, version,
completion, and upgrade itself from automatic checks/prompts. Terminal detection
alone may not identify every agent invocation; settle precise eligibility before
integration.

Write prompts/progress to standard error and preserve standard output for results.
Never consume piped input. Cache, network, and checker failures must not affect
ordinary command behavior or exit status. Report an upgrade failure after a
successful command separately, without retroactively failing the completed
operation. An explicit upgrade failure should fail the upgrade command.

## Release discovery and state

Reuse the repository and artifact contract in
[.goreleaser.yaml](../../.goreleaser.yaml). Select the latest stable release and
platform archive with semantic version comparison; never automatically downgrade
a newer installation or prerelease.

Store last check attempt, last valid release, and dismissal time/version outside
the ticket database. Atomic writes and concurrency-safe claims should suppress
duplicate daily checks across invocations. Failed checks also back off.
Handle corrupt state, clock changes, stale claims, rate limits, malformed release
metadata, and unavailable storage. Bound request durations and response sizes.
A detached checker must finish bounded work and must not recursively launch itself.

## Ownership and installation

The standalone installers should write a versioned receipt identifying the
canonical executable they own. Resolve symlinks; directory writability alone
does not establish ownership. Missing, invalid, mismatched, or package-managed
receipts require manual upgrade guidance. Existing installations could enroll by
re-running the official installer once. Keep receipts consistent with installation
success and rollback.

Download the archive and canonical checksum file from the same pinned release.
Verify the archive before extracting only the expected executable. Bound download
and extraction sizes, reject unsafe archive entries, and clean staging on failure
or cancellation. Verify the staged executable reports the expected version without
opening instance state. Expose progress callbacks to the command layer.
Checksums from the same release establish artifact consistency; this proposal
does not add independently signed release metadata.

On Linux/macOS, stage on the destination filesystem, preserve executable
permissions, and atomically replace the canonical executable in use. Retain a
recoverable old binary until validation succeeds; restore it on failure.
Report permission problems without automatically elevating privileges.

Windows needs explicit handling of running executables and file locks, potentially
with a small helper/handoff. Calling the existing PowerShell installer from the
running executable is not sufficient evidence that replacement will work.
The initiating terminal must observe actual completion/failure, not just helper
startup. Cover paths with spaces, locked files, backup recovery, and stale helper
cleanup.

## Coordination with running work

Initially defer upgrades while other work is active; never stop/restart jobs as
part of an upgrade. Ordinary processes hold shared leases keyed by canonical
executable identity, across all data-directory instances sharing that executable.
Installation requires a nonblocking exclusive lease. Use operating-system locks
with crash-safe release, transfer the initiating command's own lease without
deadlock, and prevent concurrent installers or new work during replacement.

Cover supervisors, interactive agent sessions, commands accessing instance state,
and detached helpers for their relevant lifetime. Close the supervisor launch gap:
a delayed child must not start using an old executable after replacement against
a database migrated by a newer executable. Validate binary generation after lease
acquisition where needed.

Pre-feature processes cannot participate, so initial adoption requires upgrading
while existing work is stopped. Executable rollback does not imply database
rollback; version validation must not migrate the database.

## Existing foundations and retained work

Release packaging already targets Linux, macOS, and Windows and publishes
checksums. [install.sh](../../install.sh) and [install.ps1](../../install.ps1)
already provide verified downloads and staged installation.

Three tickets had READY work at deferral:

| Ticket | Recorded commit | Branch |
| --- | --- | --- |
| #339 | `17a63e6` | `delegator/339-resolve-available-delegator-releases` |
| #341 | `0a9fed6` | `delegator/341-record-ownership-of-standalone` |
| #345 | `5f2198c` | `delegator/345-add-cross-process-upgrade-exclusion` |

Their worktrees were recorded under the Delegator data directory at
`worktrees/339`, `worktrees/341`, and `worktrees/345`.
Cancellation preserves those worktrees. Inspect the ticket records and branches
before resuming; READY does not mean merged or still current.
The other nine tickets were QUEUED. No additional feature tickets appeared in the
project listing at deferral.

## Prior research and estimate

The 2026-10-05 investigation found these precedents:

- GitHub's command-line interface (CLI), `gh`, checks during commands at most
  daily and prints notices:
  [environment documentation](https://cli.github.com/manual/gh_help_environment).
- `uv self update` supports standalone installs; other installations use their
  package manager:
  [installation guidance](https://docs.astral.sh/uv/getting-started/installation/#upgrading-uv).
- Rustup supports self-update during toolchain updates, check-only behavior, and
  explicit `rustup self update`:
  [Rustup book](https://rust-lang.github.io/rustup/basics.html#keeping-rustup-up-to-date).

These are archived research findings, not requirements to duplicate each tool.
The initial estimate was 4–7 engineering days for the complete cross-platform
feature, or 1–2 days for notification alone. Re-estimate before resuming,
especially Windows replacement, runtime coordination, and retained work.

## Validation and resumption

Use local release fixtures and disposable installations. Cover actual replacement
between two fixture versions, checksum/archive failures, permissions, cancellation,
rollback, concurrent checks/upgrades, active work across data directories, delayed
child startup, prompt acceptance/decline/snooze, offline behavior, installation
ownership exclusions, and native Windows completion/cleanup.

Before resuming, review retained branches, settle platform/eligibility details,
and re-estimate each archived change. Recreate or restart tickets only when the
user authorizes work again, preserving dependency order. Keep production changes
independently reviewable at about 200 lines or fewer.

When shipping, document user behavior and update the network/privacy inventory
for release checks/downloads separately from optional analytics. Keep detailed
guidance in docs/ and regenerate command documentation with `make docs` when
commands or flags change.
