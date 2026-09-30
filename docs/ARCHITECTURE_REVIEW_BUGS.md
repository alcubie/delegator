# Architecture review bug catalog

External static review of [alcubie/delegator](https://github.com/alcubie/delegator)
(`main` at review time, ~2026-09-28). Findings cover security, correctness,
performance, and operations. No live agent runs were performed.

This document is filed so maintainers can triage and track each item. Severity
uses High / Medium / Low relative to a **single-user local CLI** threat model
(no network daemon). There are no Critical remote/unauthenticated issues.

---

## Summary

| Severity | Count |
| --- | --- |
| High | 9 |
| Medium | 10 |
| Low | 3 |

**Overall posture:** Strong coordination design (no daemon, supervisor-claims,
SQLite WAL). Agent trust boundary is weaker than `TECHNICAL_DESIGN.md` §11
implies. Unattended queue can stall after failures/timeouts.

---

## High

### BUG-01 — ACP filesystem has no worktree allowlist

| | |
| --- | --- |
| **Lens** | Security |
| **Evidence** | `internal/handler/client.go` (`ReadTextFile` / `WriteTextFile`); `docs/TECHNICAL_DESIGN.md` §11 |
| **Status** | Open |

**Description**

`ReadTextFile` and `WriteTextFile` only require `filepath.IsAbs`, then call
`os.ReadFile` / `os.WriteFile` on any absolute path. Through Delegator's own
process, an agent can read secrets (`~/.ssh`, cloud credentials, other repos)
and write persistence files (shell rc, cron, `authorized_keys`) outside the
ticket worktree.

`TECHNICAL_DESIGN.md` §11 states the area of effect is the worktree. That claim
is false for the ACP filesystem client. `PRIVACY.md` is more accurate.

**Suggested fix**

Resolve paths with `EvalSymlinks`, require a prefix under the worktree (and any
intentional cache/data roots advertised to the session), and reject escapes.
Align §11 with actual behavior.

---

### BUG-02 — Unattended runs use `AllowAll` tool policy

| | |
| --- | --- |
| **Lens** | Security |
| **Evidence** | `internal/run/acp.go` (`handler.AllowAll()`); `internal/handler` policy |
| **Status** | Open |

**Description**

Supervised ACP sessions auto-approve every permission request for the full
timeout window (default 60 minutes). Prompt injection or a compromised / overly
capable agent can run execute and edit tools with the user's privileges and
inherited environment (`ANTHROPIC_API_KEY`, `GH_TOKEN`, etc.).

**Suggested fix**

Default to a restrictive policy (for example read/edit only). Make execute
opt-in via config. Keep interactive approval for `dg chat` if needed.

---

### BUG-03 — Cancel / timeout signals by raw PID

| | |
| --- | --- |
| **Lens** | Security / Correctness |
| **Evidence** | `internal/store` claim PID; `internal/run/alive_*.go`; `stop_unix.go`; `stop_windows.go`; `reconcile.go` |
| **Status** | Open |

**Description**

Stop kills the process group (Unix `Kill(-pgid, …)`) or Windows process tree
(`taskkill /T /F`) using the PID stored at claim. Liveness is PID-only (signal 0
/ `STILL_ACTIVE`). After supervisor exit and PID reuse within one boot,
`dg cancel` or the supervisor timeout can signal an unrelated process group.
The timeout is the documented backstop (default 60 minutes).

**Suggested fix**

Bind run identity (start time + executable path / process creation time).
Re-verify before signalling. Prefer a job object or recorded child list; on
mismatch, fail the ticket without signalling.

---

### BUG-04 — Queue stalls after failed or timed-out runs

| | |
| --- | --- |
| **Lens** | Correctness |
| **Evidence** | `internal/cli/run.go` (`Next` only when `claimed && err == nil`); `internal/run/supervisor.go` timeout path; `internal/run/reconcile.go` (`Next` only if `marked > 0`); inbox/`dg` does not call `Next` |
| **Status** | Open |

**Description**

The hidden supervisor only chains `run.Next` after a successful claim-and-run.
Setup or agent failures skip chaining. The timeout path marks the ticket failed
and kills the supervisor process group, often before `Next` runs. Reconcile only
triggers `Next` when it marked tickets in that call. Plain `dg` (inbox) runs
reconcile but does not resume an idle queue of already-failed tickets with free
capacity.

This contradicts the unattended-queue product promise: after the first
failure/timeout, work can sit queued until the user runs something that
explicitly starts more work (`dg start`, `dg ticket`, etc.).

**Suggested fix**

Chain `Next` after any claim that frees capacity; call `Next` before
process-group kill on timeout; and/or always call `Next` from `dg` / reconcile
when `ClaimableCount > 0`.

**Test gap**

No test asserts that agent failure or timeout re-chains the queue at the
`cli/run` level. Setup-failure tests intentionally assert the opposite for
setup-only faults.

---

### BUG-05 — Windows cancel can orphan agent children

| | |
| --- | --- |
| **Lens** | Correctness |
| **Evidence** | `internal/run/stop_windows.go`; `DECISIONS.md` (2026-09-15) |
| **Status** | Open |

**Description**

Windows stop uses `taskkill /T /F`. Children whose parent already exited fall
outside the current tree. Detach uses `DETACHED_PROCESS`. Cancel/timeout can
leave agent or tool processes running against the worktree; restart/chat can
race with orphans. Maintainers already documented this limitation.

**Suggested fix**

Assign a Job Object at supervisor start, or track child PIDs and kill them
explicitly.

---

### BUG-06 — `dg accept` holds SQLite writer lock across git

| | |
| --- | --- |
| **Lens** | Performance / Correctness |
| **Evidence** | `internal/cli/accept.go`; `store.ChangeStatusWith`; `project` squash/`patch-id` helpers |
| **Status** | Open |

**Description**

`ChangeStatusWith` runs `RequireBranchMerged` and `RemoveWorktree` inside the
writer transaction. Large squash equivalence checks can load full diffs and scan
many commits. Concurrent supervisors and CLI commands can hit `SQLITE_BUSY`
(busy_timeout 5s). Claim paths correctly keep git outside the writer lock;
accept does not.

**Suggested fix**

Merge-check and worktree removal outside the transaction; short status update
only. Use a two-phase accept or retry if remove fails (cancel already documents
a similar pattern).

---

### BUG-07 — ACP `Load` buffers full session replay in memory

| | |
| --- | --- |
| **Lens** | Performance |
| **Evidence** | `internal/handler/session.go` (`collect` / `Load` / `Prompt`) |
| **Status** | Open |

**Description**

Session load collects all events into `[]Event`. `Prompt` yields the full replay
before the live turn. Long resumed sessions grow heap with history size,
independent of the bounded live event channel (`eventRoom=64`).

**Suggested fix**

Stream replay to the log without retaining, or bound/truncate replay for
supervised (non-interactive) runs.

---

### BUG-08 — `ReadTextFile` loads whole files with no size cap

| | |
| --- | --- |
| **Lens** | Performance / Security |
| **Evidence** | `internal/handler/client.go` (`os.ReadFile` → `string(b)`); `docs/TOKEN_COSTS.md` |
| **Status** | Open |

**Description**

There is no byte or line limit on ACP file reads. An agent can request huge
files, burning supervisor memory and agent tokens. Prompt text asks agents not
to read large files; the client does not enforce it.

**Suggested fix**

Refuse or truncate above a configured byte/line limit with a clear error.

---

### BUG-09 — Worktrees, run logs, and project cache grow without GC

| | |
| --- | --- |
| **Lens** | Ops / Performance |
| **Evidence** | Cancel keeps worktrees (`DECISIONS` / cancel command); `RemoveWorktree` on accept only; run NDJSON logs; `ProjectCache`; FEATURES GC deferred |
| **Status** | Open |

**Description**

Cancelled and failed tickets can leave full checkouts under `worktrees/<id>`.
Run logs and `cache/` are never pruned by the app. Many tickets against large
repos produce multi-GB growth with no `dg gc` (or equivalent).

**Suggested fix**

Ship age/size-based cleanup for cancelled worktrees, closed run logs, and
project cache. Document the disk model in FEATURES.

---

## Medium

### BUG-10 — Agent inherits full env and full `dg` CLI

| | |
| --- | --- |
| **Lens** | Security |
| **Evidence** | `handler/session.go` env; supervisor prompt (`dg show` / `dg finish`); `cli/rpc.go` refused-methods set |
| **Status** | Open |

**Description**

Agent processes inherit `os.Environ()`. The prompt teaches `dg` with
`--data-dir`. Nothing prevents `dg agents add`, `dg cancel`, `dg ticket`, or
hidden `dg run` via JSON-RPC. A malicious or injected agent can persist a custom
agent binary, start more runs, or cancel others.

**Suggested fix**

Allowlist environment variables. Ship a restricted agent-mode CLI (only
`show`/`finish` for the claimed ticket). Refuse `run` / `agents.*` / `config.set`
over RPC by default.

---

### BUG-11 — ACP writes use `0644` / `0755` vs claimed `0600` / `0700`

| | |
| --- | --- |
| **Lens** | Security |
| **Evidence** | `handler/client.go` `MkdirAll(0o755)` / `WriteFile(…, 0o644)`; `TECHNICAL_DESIGN.md` §11; `PRIVACY.md` |
| **Status** | Open |

**Description**

Docs claim Delegator-written files use owner-only modes. ACP-driven writes use
world/group-readable modes, which matters on multi-user hosts when writes land
outside the private data directory.

**Suggested fix**

Use `0600` / `0700` for ACP writes (or inherit owner-only defaults consistently).

---

### BUG-12 — Installer checksum TOFU; no signature; symlink member risk

| | |
| --- | --- |
| **Lens** | Security / Ops |
| **Evidence** | `install.sh`; `.github/workflows/release.yml` |
| **Status** | Open |

**Description**

The installer verifies SHA-256 against a checksum file from the **same** GitHub
release (trust-on-first-use). There is no cosign/GPG attestation. Archive member
`dg` is not checked to be a regular file (symlink possible). `tag_name` is parsed
from release JSON with `sed`.

**Suggested fix**

Add Sigstore/cosign attestations; after extract require a regular non-symlink
file; prefer `jq` (or a stricter parser) for JSON. Keep the draft→verify→publish
release chain.

---

### BUG-13 — No PR CI for Go tests / `make check`

| | |
| --- | --- |
| **Lens** | Ops / Security |
| **Evidence** | `.github/workflows/docs.yml` (PRs); `release.yml` (`make check` on tags) |
| **Status** | Open |

**Description**

Pull requests only run documentation checks. Go tests and `make check` run on
release builds. Regressions or malicious PRs can land on `main` without
automated application gates.

**Suggested fix**

Add a PR workflow running `make check` (or at least `go test ./...` +
staticcheck).

---

### BUG-14 — Cancelled dependencies permanently block dependents

| | |
| --- | --- |
| **Lens** | Correctness |
| **Evidence** | Store `claimableRule`; `depend` docs |
| **Status** | Open |

**Description**

Claim eligibility requires dependency status `done`. A cancelled prerequisite
stays a blocker until the dependency link is removed. Dependents remain queued
with no auto-unblock. Easy operational footgun.

**Suggested fix**

Surface clearly in the inbox; optional auto-drop or explicit warn when
cancelling a depended-on ticket.

---

### BUG-15 — Hidden `dg run <id>` bypasses capacity and deps

| | |
| --- | --- |
| **Lens** | Correctness |
| **Evidence** | `store.Claim` / `Start` vs `ClaimNext`; `cli/run.go` |
| **Status** | Open |

**Description**

Explicit ID start checks state transitions only. `ClaimNext` enforces free slots
and dependency eligibility. Manual/hidden `dg run <id>` can oversubscribe
`runs` / per-project limits or start blocked tickets.

**Suggested fix**

Apply the same eligibility checks to explicit `Start`, or refuse when over
capacity / blocked.

---

### BUG-16 — Shared project cache across concurrent tickets

| | |
| --- | --- |
| **Lens** | Correctness |
| **Evidence** | Project cache path; ACP `AdditionalDirectories`; `AllowAll` |
| **Status** | Open |

**Description**

One cache directory is shared across concurrent tickets for the same project.
With `runs` / `max_runs_per_project` > 1 and full tool access, agents can
corrupt shared build caches.

**Suggested fix**

Document as non-isolation; optional per-ticket cache; scrub on accept.

---

### BUG-17 — Missing indexes on hot ticket/run predicates

| | |
| --- | --- |
| **Lens** | Performance |
| **Evidence** | `internal/store/migrations.go`; inbox ticket query correlated subqueries |
| **Status** | Open |

**Description**

No `tickets(status)` or `runs(ticket_id)` indexes. Inbox/list queries use
correlated subqueries for latest run and transition times per open ticket. Fine
at small scale; degrades as history grows.

**Suggested fix**

Add `CREATE INDEX tickets_status ON tickets(status)` and
`CREATE INDEX runs_ticket ON runs(ticket_id, id)`; consider denormalizing
timestamps onto tickets.

---

### BUG-18 — Scheduler gap until a human runs `dg`

| | |
| --- | --- |
| **Lens** | Ops |
| **Evidence** | `docs/DAEMON.md`; `withStore` → `Reconcile` |
| **Status** | Open |

**Description**

No daemon by design. After reboot or a dead supervisor, nothing reconciles until
the next command. `DAEMON.md` recommends a login/timer unit that is not shipped.
Combined with BUG-04, queues can stay idle longer than users expect.

**Suggested fix**

Ship optional systemd/launchd timer units before inventing an in-process daemon.

---

### BUG-19 — Design docs drift from multi-agent ACP reality

| | |
| --- | --- |
| **Lens** | Ops / Maintainability |
| **Evidence** | `TECHNICAL_DESIGN.md` (adapters / claude-only era; `dg project relink`); FEATURES leftover `result`/`flags` mentions; `root.go` command tree |
| **Status** | Open |

**Description**

Design docs still describe prototype-era surfaces that the code no longer ships
(for example `adapters/`, unimplemented `project relink`). Reviewers and agents
following docs alone will mis-model the system.

**Suggested fix**

Refresh the design corpus to match the ACP agent registry and current CLI.

---

## Low

### BUG-20 — Local RPC is a full privileged control plane (no auth)

| | |
| --- | --- |
| **Lens** | Security |
| **Evidence** | `internal/cli/rpc.go`; OpenRPC surface |
| **Status** | Open |

**Description**

`dg rpc` is stdin/stdout JSON-RPC with no authentication. Expected for a
single-user CLI, but easy to overestimate as a bounded API. Any local code
execution as the user (or a confused deputy that can spawn `dg rpc`) gets full
queue control.

**Suggested fix**

Document the threat model; refuse destructive/hidden methods by default;
optional capability token.

---

### BUG-21 — `dg search` reads every prose file

| | |
| --- | --- |
| **Lens** | Performance |
| **Evidence** | `internal/cli/search.go` |
| **Status** | Open |

**Description**

Search loads all tickets then `ReadFile` + lowercase per prose file. No FTS.
Linear in ticket count × prose size.

**Suggested fix**

SQLite FTS5 over titles/prose, or ripgrep over `tickets/`.

---

### BUG-22 — PID-only liveness can hold a slot until timeout

| | |
| --- | --- |
| **Lens** | Correctness |
| **Evidence** | `alive_unix.go`; `alive_windows.go`; `reconcile` `running()` |
| **Status** | Open |

**Description**

Intra-boot PID reuse can make a dead run look alive until `timeout_minutes`.
Related to BUG-03; rarer than wrong kill, but can stall capacity.

**Suggested fix**

Same process-identity binding as cancel/stop hardening.

---

## Recommended remediation order

1. Path-confine ACP FS + fix write modes; tighten default `AllowAll` (BUG-01, BUG-02, BUG-11).
2. Chain `Next` after fail/timeout; resume from inbox when claimable (BUG-04).
3. Bind stop/cancel to process identity; Job Object on Windows (BUG-03, BUG-05, BUG-22).
4. Move accept git work outside the SQLite writer (BUG-06).
5. Cap ACP reads; drop full replay buffer; add disk GC (BUG-07, BUG-08, BUG-09).
6. PR `make check`; release signing beyond same-channel SHA-256 (BUG-12, BUG-13).
7. Optional login/timer unit; refresh `TECHNICAL_DESIGN.md` (BUG-18, BUG-19).

---

## What looks solid (non-bugs)

These are strengths noted during the same review, not defects:

- No first-party network listener or telemetry surface.
- Supervisor-claims with PID written in the same transaction as claim.
- SQLite WAL, `_txlock=immediate`, `busy_timeout`, parameterized queries.
- Ready holds a capacity slot (review backpressure).
- Dependency cycle detection; accept merge / stable `patch-id` squash proof.
- Data-dir create with `0600` before SQLite open; run logs `O_EXCL` `0600`.
- Git invoked without a shell; dangerous `GIT_*` overlays stripped.
- Release draft → download → checksum verify → undraft.
- Coverage floor, staticcheck, OS-specific process tests, `dg-fake-agent`.

---

## Review method

- Cloned public `main`; read design docs (`TECHNICAL_DESIGN`, `DAEMON`,
  `RUN_CONTROL`, `DECISIONS`, `PRIVACY`, `SECURITY`) and traced
  `internal/{cli,store,run,handler,project}`.
- Static analysis only; no production data or live coding-agent sessions.
