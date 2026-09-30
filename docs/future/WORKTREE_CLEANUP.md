# Scheduled worktree cleanup

**Status:** Deferred proposal; not implemented.
**Date:** 2026-09-30

Separate ticket closure from worktree deletion, and remove expired worktrees
automatically through the operating system's scheduler. Cleanup should happen
behind the scenes, without a public `dg cleanup` command or manual maintenance.
This document records future work only; it does not change current behavior.

## Current behavior and failure

`dg accept` checks that the ticket branch is merged and removes its worktree
inside the callback of a database status transaction. A removal error rolls the
ticket back to READY, but the database cannot undo filesystem deletion or restore
Git's worktree registration. A retry tolerates an absent directory, but an existing
directory without Git registration causes Git to report "not a working tree".

Ticket 296 exposed this state: shell history line 2814 contained `dg accept`, the
ticket remained READY, its branch was merged, and Git no longer listed its
worktree. Only `.astro` and `node_modules/.vite` artifacts remained. An earlier
`npm run dev` may have recreated files during removal; the original command's
error was unavailable, so that cause remains unconfirmed.

`dg cancel` intentionally retains worktrees so unfinished changes remain available
for inspection. Those worktrees currently have no automatic expiry.

The relevant code is in [accept.go](../../internal/cli/accept.go),
[cancel.go](../../internal/cli/cancel.go),
[run.go](../../internal/run/run.go), and
[store.go](../../internal/store/store.go).

## Proposed lifecycle

Acceptance should validate merge status and uncommitted changes, then durably
mark the ticket DONE. Cancellation should stop the running agent when necessary,
then durably mark the ticket CANCELLED. Both should retain their worktrees for a
configured period. Ticket closure should release queue capacity independently of
cleanup success.

Use the recorded transition into DONE or CANCELLED to calculate expiry. Reboots
must not reset retention. Keep retention separate from `done_hours`, which only
controls how long accepted tickets appear in the inbox. Separate retention settings
for accepted and cancelled worktrees would allow more time to inspect cancelled
work. One day for accepted work and seven days for cancelled work are possible
defaults, not agreed values.

A short internal worker should scan for eligible worktrees, perform cleanup, and
exit. Retain enough durable cleanup state to resume after interruption and expose
the last failure or reason for deferral through existing ticket details. Expiry
makes a worktree eligible; it does not guarantee deletion at that exact time.

## Scheduling across reboots

Setup should install and enable one recurring job for each Delegator data directory.
An hourly scan is a candidate schedule. The scheduler should invoke an internal
entry point using stable executable and data-directory paths, without depending
on an interactive shell or the current working directory.

On Linux with systemd, use an enabled user calendar timer with `Persistent=true`.
This records the last trigger on disk and causes a catch-up activation if a run
was missed while the timer was inactive. A normal user manager starts at login;
lingering permits it to start at boot and remain active after logout. Catch-up at
the next login is the proposed default; cleanup does not need to run before login.
See the upstream [timer documentation](https://github.com/systemd/systemd/blob/main/man/systemd.timer.xml)
and [user manager documentation](https://github.com/systemd/systemd/blob/main/man/loginctl.xml).

Windows Task Scheduler provides missed-run handling through
[`StartWhenAvailable`](https://learn.microsoft.com/en-us/windows/win32/taskschd/tasksettings-startwhenavailable).
The macOS scheduler integration and its reboot behavior still need design and
validation. Systems without a supported scheduler should clearly report that
automatic cleanup is unavailable.

Installation, upgrades, and removal must manage the scheduled job's lifecycle.
The worker should run as the data directory's owner. Reuse the approach in
[the daemon discussion](../DAEMON.md): the operating system triggers short work;
Delegator does not need a permanent process for cleanup.

## Cleanup boundaries and retries

- Limit automatic cleanup to worktrees of DONE and CANCELLED tickets. Preserve
  active tickets, including READY and FAILED work that may still need inspection.
- Preserve uncommitted changes by default and report why cleanup was deferred.
  Retention expiry alone must not authorize discarding unfinished work.
- Preserve branches, ticket records, and logs. Shared project caches need a
  separate retention policy and are outside this proposal.
- Check Git registration and directory contents separately. Treat an already
  removed worktree as complete, and recover from partial removal on later runs.
  Preserve unexpected leftovers unless their ownership and disposable nature can
  be established; a missing `.git` file alone is insufficient justification for
  recursive deletion.
- Recheck eligibility and coordinate concurrent workers before deletion. Avoid
  holding the database writer lock through filesystem cleanup.
- Treat locked files, active writers, unavailable repositories, and permission
  errors as deferrals or failures that can be retried. Do not automatically kill
  user-started dev servers. Cleanup errors must never reverse ticket closure.

## Open decisions and validation

Before implementation, settle retention defaults and configuration, treatment of
dirty cancelled worktrees, safe handling of orphaned artifacts, and migration of
existing terminal tickets. Legacy tickets without a reliable closure timestamp
must not become immediately eligible merely because that timestamp is missing.
Existing READY tickets affected by partial acceptance need separate recovery;
the terminal-ticket cleanup job must not silently accept them.

Regression coverage should include partial Git removal, artifact recreation by
an active writer, missing directories and registrations, dirty cancelled work,
concurrent cleanup attempts, and crashes between deletion and recording success.
Verify that schedules survive reboot, missed runs catch up, and repeated failures
remain visible while the queue continues normally.

## Current workaround limits

`dg accept --force` skips the merge check and permits removal of uncommitted
changes. It still invokes Git's worktree removal, so it does not bypass the
"not a working tree" error when only an unregistered directory remains.

For the observed ticket 296 state, stop any remaining dev server and preserve the
leftover directory by moving it aside before retrying `dg accept 296`. The current
implementation already tolerates an absent worktree directory. This is a manual
recovery note, not a change to the deferred design or an action performed as part
of documenting it.
