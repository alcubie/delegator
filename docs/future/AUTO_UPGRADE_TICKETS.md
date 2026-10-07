# Deferred self-upgrade ticket breakdown

**Deferred:** 2026-10-06 at the user's request.
**Design:** [Optional self-upgrades](AUTO_UPGRADE.md).

This archive preserves tickets #339–#350 so the feature can be resumed without
keeping it in the active queue. Ticket numbers are historical references, not
instructions to execute the work. Cancellation retains ticket records and worktrees.

## Dependency overview

| Ticket | Work | Depends on | Estimated production lines |
| --- | --- | --- | --- |
| #339 | Resolve available Delegator releases | — | 130–180 |
| #340 | Persist update check state | #339 | 120–180 |
| #341 | Record ownership of standalone installations | — | 150–200 |
| #342 | Stage verified release downloads | #339 | 160–200 |
| #343 | Replace standalone Unix executables safely | #341 #342 | 120–180 |
| #344 | Replace standalone Windows executables safely | #341 #342 | 160–200 |
| #345 | Add cross-process upgrade exclusion primitives | — | 120–190 |
| #346 | Coordinate running Delegator processes with upgrades | #345 | 140–200 |
| #347 | Expose explicit upgrades through dg upgrade | #340 #343 #344 #346 | 150–200 |
| #348 | Check for updates during interactive use | #340 #347 | 120–180 |
| #349 | Offer confirmed upgrades after interactive commands | #348 | 140–200 |
| #350 | Verify the complete cross-platform upgrade flow | #349 | 0–80 |

Production estimates exclude tests and generated files. Keep each change
independently reviewable, aiming for no more than 200 production lines; split
further before expanding scope. Follow repository instructions and run
`make docs` when commands or flags change. Use focused tests with injected
networking, clocks, filesystem failures, and process behavior as appropriate;
never download real releases or overwrite the developer's executable in tests.

## Original ticket scopes

The ticket-specific bodies below preserve scope and acceptance/testing expectations.
Their repeated scope-discipline paragraph is consolidated above. READY/QUEUED
labels record state before deferral, not current queue status.

### #339: Resolve available Delegator releases

**Depends on:** None. **State before deferral:** READY.

Estimated production diff: 130–180 lines; tests/generated files excluded.

Introduce the internal release lookup foundation, without command hooks or installation. Use the existing GitHub release repository and canonical artifact naming in .goreleaser.yaml. Resolve the latest stable release, compare semantic versions correctly, and select the archive for the current operating system and architecture. Skip automatic offers for development/snapshot builds and do not downgrade prereleases or newer versions. Bound request duration and response sizes; handle unavailable releases, malformed metadata, rate limits, and unsupported platforms. Return a typed release description usable by later download and notification work. Verify these cases using a local test server.

### #340: Persist update check state

**Depends on:** #339. **State before deferral:** QUEUED.

Estimated production diff: 120–180 lines; tests/generated files excluded.

Add a small per-user update-state store outside the ticket database, independent of project initialization and database migrations. Record last check attempt, last valid available release, and prompt dismissal time/version. Use atomic writes and concurrency-safe claiming to allow at most one automatic release check per 24 hours across simultaneous invocations; failures also back off, while a manual upgrade may refresh immediately. Handle corrupt state, clock changes, stale claims, and unavailable storage without failing ordinary commands. Define DG_NO_UPDATE_CHECK to disable automatic checks and prompts. No command integration yet; test with an injected clock.

### #341: Record ownership of standalone installations

**Depends on:** None. **State before deferral:** READY.

Estimated production diff: 150–200 lines; tests/generated files excluded.

Have install.sh and install.ps1 record a minimal versioned installation receipt identifying the canonical installed executable and standalone installer ownership after a successful installation. Implement the small receipt reader/eligibility check needed by the updater. Resolve symlinks consistently and do not infer ownership from directory writability alone. Missing, invalid, mismatched, or package-managed receipts must produce manual upgrade guidance, never silently replace a binary. For existing installations without a receipt, document re-running the official installer once to enroll; development builds remain excluded. Keep receipt persistence consistent with installer failure/rollback behavior. Extend installer fixtures and document this narrow installation change. No update command yet.

### #342: Stage verified release downloads

**Depends on:** #339. **State before deferral:** QUEUED.

Estimated production diff: 160–200 lines; tests/generated files excluded.

Download the selected archive and its canonical checksum file from the same pinned release, using the existing release naming contract. Verify the archive checksum before extraction; extract only the expected dg executable into temporary staging. Bound network requests and extraction sizes, reject unsafe archive entries, support Unix tar archives and Windows zip archives, and clean temporary files on cancellation/failure. Verify the staged executable reports the expected version without opening the ticket database. Expose progress callbacks for download bytes and verification phases; no terminal rendering or executable replacement. Test corrupted downloads, missing/ambiguous checksums, hostile archives, wrong reported versions, cancellation, and both archive formats.

### #343: Replace standalone Unix executables safely

**Depends on:** #341 #342. **State before deferral:** QUEUED.

Estimated production diff: 120–180 lines; tests/generated files excluded.

Implement the Linux/macOS replacement backend using the ownership receipt and a verified staged executable. Replace the canonical executable currently being used, staging on its filesystem and preserving executable permissions. Use atomic replacement, preserve a recoverable old binary until validation succeeds, and restore it on replacement/validation failure. Do not request elevated privileges automatically. Keep the receipt consistent with installation success and prevent concurrent installers from racing through the backend contract. Return precise phase/errors for the command layer. Tests must cover permissions, symlinks, interrupted staging, successful replacement, and rollback. No public command or runtime coordination wiring yet.

### #344: Replace standalone Windows executables safely

**Depends on:** #341 #342. **State before deferral:** QUEUED.

Estimated production diff: 160–200 lines; tests/generated files excluded.

Implement the Windows replacement backend for the same updater contract. Handle the currently executing dg.exe and Windows file locking explicitly, using a narrowly scoped helper/handoff if required; do not assume invoking the existing PowerShell installer from dg is sufficient. Preserve the old executable until successful replacement/validation, update the receipt consistently, and clean or recover stale backup/helper files. Keep progress and final success/failure observable by the initiating terminal and never report success merely because a helper started. Cover paths with spaces, locked files, permission failures, cancellation, replacement failure, and cleanup in native Windows tests. No public command wiring. Split helper lifecycle into a dependent ticket if it cannot fit the production estimate.

### #345: Add cross-process upgrade exclusion primitives

**Depends on:** None. **State before deferral:** READY.

Estimated production diff: 120–190 lines; tests/generated files excluded.

Add a narrowly scoped cross-platform coordination primitive supporting shared leases for processes using an executable and an exclusive upgrade lease. Key it by canonical executable identity so different --data-dir instances sharing one installation cannot upgrade over each other. Ordinary leases must coexist; exclusive acquisition must be nonblocking and explain that work is active. Use operating-system locking with crash-safe release, not an unchecked stale lock file. Support the initiating command handing over its own lease without deadlock. Define the contract for preventing new work during replacement; no command/runtime wiring yet. Test contention, process death, separate installations, and competing upgrades. Document that pre-feature processes cannot participate and require an initial quiescent upgrade.

### #346: Coordinate running Delegator processes with upgrades

**Depends on:** #345. **State before deferral:** QUEUED.

Estimated production diff: 140–200 lines; tests/generated files excluded.

Wire the shared lease into application/runtime entry points so background supervisors, interactive agent sessions, commands accessing instance state, and detached helpers cannot overlap an exclusive upgrade unsafely. Retain leases for the relevant full process lifetime, including commands that return errors. Coordinate the supervisor launch gap so a delayed child cannot reopen the database under an old binary after replacement; validate the running binary generation after acquiring its lease where needed. An exclusive upgrade should defer while other work is running, never terminate or restart user jobs. Cover concurrent invocations, delayed child startup, multiple --data-dir instances sharing a binary, and completion/error cleanup. No upgrade command yet. Split further if the runtime integration exceeds 200 production lines.

### #347: Expose explicit upgrades through dg upgrade

**Depends on:** #340 #343 #344 #346. **State before deferral:** QUEUED.

Estimated production diff: 150–200 lines; tests/generated files excluded.

Add dg upgrade as the common user-facing entry point for a fresh version check and installation. Reuse the release client, receipt eligibility, verified staging, platform replacement backend, and exclusive runtime lease. Calling dg upgrade explicitly authorizes installation; ordinary automatic prompts will call the same service only after confirmation. Report already-current versions, unsupported installation ownership, unwritable destinations, active-work deferral, and failures clearly. Render download byte/percentage progress followed by verification, installation, and final installed-version confirmation on standard error. Preserve the old working binary on failure and release locks on cancellation. Work without project initialization and avoid opening/migrating the ticket database. Never expose self-upgrade through remote command dispatch. Refresh command reference with make docs; add explicit command behavior tests.

### #348: Check for updates during interactive use

**Depends on:** #340 #347. **State before deferral:** QUEUED.

Estimated production diff: 120–180 lines; tests/generated files excluded.

Integrate cached release discovery into eligible interactive invocations. Use a bounded detached checker if necessary so short commands can finish without waiting for networking and still populate the cache for the next invocation. Apply the shared daily claim/backoff; avoid recursion through the checker itself. Respect DG_NO_UPDATE_CHECK and exclude development builds, noninteractive/agent calls, structured output, remote command dispatch, background workers, help, version, shell completion, and upgrade itself. A cache/network/helper failure must not change the user's command output or exit status. Add any hidden command with general descriptions and regenerate command docs as required. Tests should prove short commands do not wait for slow networks and simultaneous invocations make only one check.

### #349: Offer confirmed upgrades after interactive commands

**Depends on:** #348. **State before deferral:** QUEUED.

Estimated production diff: 140–200 lines; tests/generated files excluded.

After an eligible successful interactive command finishes, use the cached release to display installed/available versions and ask Upgrade now? [y/N]. Require an interactive input/output context, preserve standard output for command results, and do not consume piped input. Empty input, end of input, cancellation, and No must not install; decline snoozes prompts for 24 hours. Yes invokes exactly the shared upgrade service and progress renderer from dg upgrade. Do not rerun the original command; explain the next invocation uses the new version. Defer installation while other work is active, and give manual guidance for installations without updater ownership. An update failure after a completed command must be reported distinctly without turning that completed operation into a failed command. Test affirmative, negative, malformed input, cancellation, snooze, automatic-check opt-out, and all automation exclusions.

### #350: Verify the complete cross-platform upgrade flow

**Depends on:** #349. **State before deferral:** QUEUED.

Estimated production diff: 0–80 lines; tests/generated files excluded.

Add focused end-to-end coverage of the assembled feature using local release fixtures and disposable executable installations. Exercise normal command -> cached notice -> Yes -> visible progress -> new version; No/snooze; no-network behavior; package/development install exclusion; concurrent checks/upgrades; active-job deferral across data directories; checksum failure; installation failure/rollback; and native Windows helper/replacement behavior. Use two fixture release versions so assertions verify actual replacement. Run applicable Unix and Windows checks, documenting any platform validation still unavailable. Add concise user guidance in docs/ covering daily checks, dg upgrade, prompt timing, opt-out, receipt enrollment for existing installations, progress/failure recovery, and active-work deferral. Update the network/privacy inventory for GitHub release checks/downloads without conflating them with optional analytics. Keep README minimal; refresh generated command reference if needed. This ticket owns integration verification/documentation only, not deferred production components.
