# Delegator: features that wait

**Condition:** live list
**Date:** 2026-08-17
**Companion to:** [`TECHNICAL_DESIGN.md`](TECHNICAL_DESIGN.md)
**Language:** ASD-STE100 Simplified Technical English, Issue 9.

This document holds each feature that version 1 does not have. Section 0 of the technical
document declares the technical names, and this document uses the same names.

An item is here because it is not necessary now. An item here is not a rejected item. Each
item has a **trigger**: the signal that shows that the time for the item came. If no
trigger occurs, the item stays here, and this is a good result.

## 1. The interface

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| The TUI | A full screen program with the inbox on the left and the ticket on the right. It refreshes while you look at it. It goes in `internal/tui/`, and it reads the same structure from `inbox/`. | The commands `dg` and `dg show` give the same data. The TUI was about 40 percent of the work of version 1. | READY has more than about 20 tickets, and the output of `dg` is difficult to scan. |
| Keys for commands | Each command of the person also gets one key in the TUI. The config in §9.2 keeps its place for this. | `dg open <name> <id>` does the same work from the CLI. | The TUI arrives. |
| Reorder with `j` and `k` | Move one ticket up or down in the queue with one key. | `dg queue` opens the queue file in `$EDITOR`, and the queue file is a list of ids. | The TUI arrives. |
| A GUI | A graphical interface. In Go it is a second binary `cmd/dg-gui/`. In a different language it uses `dg --json`. | A terminal is sufficient for a programmer, and the CLI is the product now. | A person who does not use a terminal must use delegator. |
| A filter by project | Show one project only. | The inbox shows the project on each row, and other programs can filter the output. | You have more than about 5 projects with active tickets. |

## 2. More work at the same time

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| More than one run | Two or more agents operate at the same time, in different worktrees. | One run at a time removes the control of slots, and each race between two supervisors. The config holds the value `runs = 1`, so a change is small. | You wait for the queue, and the cost of the agent is acceptable. |
| A limit for each project | A maximum count of runs for one project. | This is necessary only after more than one run at a time. | Two runs in one project write the same files. |

## 3. Other agents

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| The codex, opencode and gemini adapters | One adapter for each agent, in the package `adapters/`. | The prototype gave three adapters that no person operated. Version 1 gives claude only, and the interface in §6.6 keeps the seam. | You want a different agent, or claude is not available. |
| A conformance test set | A set of tests that each adapter must pass before its release. It makes sure that the adapter accepts a session id, uses the worktree, and reports a non-zero exit code. | One adapter does not need a conformance test set. | The second adapter starts. |

## 4. Installation

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| A `.pkg` file for macOS | The installer that a person opens with two clicks. | The command `curl -fsSL … \| sh` operates on Linux and on macOS. A `.pkg` file also needs a signature from Apple, and a check by Apple. | A person who is not a programmer must install delegator. |
| A Homebrew formula | Installation with `brew install`. | The same command above does this work. | Persons ask for `brew`. |
| `dg upgrade` | A command that gets the new binary. | The installer command does this work, and one method is easier to keep correct than two. | The installer command is not available, or an upgrade must keep data. |

## 5. Measurements

The prototype had these measurements. Version 1 has none of them, because they are not
necessary for the work of the product.

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| Data for each run | The duration, the cost, the token counts and the exit code. | This data does not change what the person does next. | The cost of the agent is a problem, and you want to find its source. |
| Data for each period at the inbox | The count of tickets at the start, the count of closures, and the time of the last input. | This data exists to give a correct value to a limit that version 1 does not have. | The limit on READY comes back. See §6. |
| The history of the config | Each change to a value, with its time. | Nothing analyses it now. | A measurement above arrives, because a measurement without this data has no control variable. |
| `dg stats` | A command that reads the measurements. | There is no data to read. | A measurement above arrives. |

**A note on the sequence.** You cannot get this data later. If a measurement arrives, it
must write its data from that day forward, and the data before that day does not exist.

## 6. Control of the READY list

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| A limit on READY | A maximum count of tickets in READY. Delegator holds each other completed ticket in a different state, and releases it when the person closes one. | Tickets collect in READY, as mail collects in a mail inbox. The count can be 1 or 100. | A large READY list stops you, and you accept tickets without a look at them. |

## 7. The output of the agent

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| The output with a structure | Code that reads the output of the agent and makes events from it. | The person does not see the output. Delegator gives the session id to the agent, so no code reads the output. The log file keeps the raw output. | The person wants data from inside a run, and the commit messages do not have it. |
| A live look at a run | A command that shows the output of a run while it operates. | The product is for a person who goes away. A live look is the behaviour that the product removes. | You cannot find why a run stops, and the log file is not sufficient. |
| The record of changes in sequence | A short list of each step that the agent did, in time order. | The `result`, the `flags` and the commit messages give this data in a shorter form. | The `result` and the commit messages are not sufficient to start work again. |

**A note on the research.** `docs/RESEARCH.md` gives evidence about the most useful cue
for a return to work. That cue is a record of changes in time order. Version 1 gives the
cue with the commit messages on the branch, and with `result` and `flags`. If a return to work is slow,
this item is the first one to examine again.

## 8. Safety

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| A container for each run | The agent operates in a container, and not on the computer of the person. | A worktree gives isolation of files, but it is not a sandbox. Version 1 says this directly. | An agent changes data outside its worktree, or you give delegator work that you do not trust. |
| A limit on the commands of the agent | A list of commands that the agent can start. | The agent operates with its permission questions off, because a question stops a run that has no person at it. | The item above arrives, or an agent does damage. |

## 9. Housekeeping

| Item | What it is | Why it waits | Trigger |
|---|---|---|---|
| Garbage collection of worktrees | A command that removes each worktree that a crash left behind, if the worktree has no changes. | The reconcile marks the run `failed`, and `dg accept` removes the worktree. A worktree stays only after a crash. | The worktree directory becomes large. |
| A report if the base branch moved | Delegator says that the default branch moved after the start of the run. | The person selects the diff command, so the person can see this. | A diff gives a result that is difficult to read, because the base branch moved. |
| Removal of old logs | A command that removes the log files of closed tickets. | A log file is small. | The data directory becomes large. |
| One step for the first release | Delegator makes the database with one step. The list `migrations` holds that step only, and a new database is at version 1. | No person has delegator now. The steps of the prototype are a record of the work, and no database of a person must go through them. A step that a person has must not change, so this work is possible one time only. | The first release. Do this work before the first person installs delegator. |

## 10. Smaller items

| Item | Why it waits | Trigger |
|---|---|---|
| `dg note` for the agent | The person does not see the output, and the detail belongs in a commit message. | The commit messages do not hold the data that a return to work needs. |
| `--fresh` on `dg restart` | One restart behaviour is easier to understand. A restart continues the same session. | A session becomes damaged, and only a new session can correct it. |
| A timeout for one ticket | One value in the config is sufficient. | One ticket type always goes above the limit, or always stops early. |
| A `priority` field | The person changes the order of the queue file directly. | The order of the queue is not sufficient control. |
| An automatic restart after an error | The person must see an error. An automatic restart can add cost on a ticket that is broken. | Errors from outside are frequent, and each one is a delay for you. |
| Input of the tickets of the prototype | You will finish those tickets with the prototype. | Not applicable. |
| An MCP server | The agent uses the CLI, and each agent can start a program. A server adds a port, a token and a life to control. | An agent cannot start a program, or a different program must control tickets. |

## 11. The parent product

These items belong to the spine product, and not to delegator. See
`../docs/pm/business-case.md`.

- Work in the cloud, and a dashboard on a web page.
- More than one person, and teams.
- Input from GitHub, from JIRA, from webhooks or from a schedule.
- Deployment to a test system before release, automatic merge, or control of a CI system.
- Lower cost from batch APIs, or from a selection between models.
- Tasks that are not code.
