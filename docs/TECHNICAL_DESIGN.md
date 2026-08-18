# Delegator: technical document

**Condition:** draft, for your examination
**Date:** 2026-08-17
**Product:** `delegator`, an Alcubi product. The repository is `github.com/alcubie/delegator`.
**Replaces:** the prototype at the root of this repository. Its MODE file contains
`prototype`.
**Language:** ASD-STE100 Simplified Technical English, Issue 9.

## 0. Technical names

ASD-STE100 approves 803 words. The dictionary is for aircraft maintenance data, and it
contains no words for software. This document therefore declares the technical names in
the table below. It uses each name for one thing only, in all of the text. Each name in
`code marks` is an identifier in the code, and is also a technical name.

| Technical name | What it is in this document |
|---|---|
| adapter | The code that operates one agent. |
| agent | An external command line program that writes code. Version 1 has claude only. |
| CLI | Command Line Interface. |
| config | The file that holds the selections of the person. |
| DONE | The group in the inbox that contains each closed ticket. |
| flags | A short note from the agent about each item that did not go as expected. The value `none` shows that there is no such item. |
| Go | The programming language of delegator. |
| goreleaser | The tool that makes the binary files and the installer. |
| hash | A short string that comes from a longer string, and is different for each input. |
| inbox | The one ordered list of tickets that the person examines. |
| lock | A file lock that gives one writer at a time. |
| log | The output of an agent, kept in a file. |
| project | One git repository that contains work. |
| project key | The unique name of a project in the data directory. |
| prototype | The throwaway code at the root of this repository. |
| queue | The ordered list of tickets that wait for work. |
| QUEUED | The group in the inbox that contains each ticket in the queue. |
| READY | The group in the inbox that contains each completed ticket. |
| result | A short note from the agent about what it did. |
| run | One execution of an agent for one ticket. |
| RUNNING | The group in the inbox that contains the ticket with an active run. |
| schema | The version number of the format of a ticket file. |
| session | One conversation with an agent, which the agent can continue later. |
| state | The condition of a ticket. |
| supervisor | The short program that operates one run and then stops. |
| ticket | One item of work, kept in one directory with two files. |
| timeout | A time limit. After the limit, the supervisor stops the run. |
| TUI | Terminal User Interface. Version 1 does not have one. |
| variables | The four items that connect a ticket to its work: the ticket file, the worktree, the branch and the session. |
| worktree | A git worktree. |

## 1. Function of the product

Delegator gives a long task to an agent, and then tells the person when the task is
complete. The person writes a ticket, goes away, and comes back to one ordered inbox.

Delegator does one thing: it controls the queue and the isolation of the work. It does
not write code. It does not do a diff. It does not examine the work. It does not control
the configuration of an agent. Each of those tasks belongs to a program that the person
selects.

The task can be of any size, and its duration can be long. The person can be a
programmer, but this is not a condition. The only condition is a git repository.

## 2. Principles of the product

These principles come from [`../docs/RESEARCH.md`](../docs/RESEARCH.md) and from the
approved [business case](../docs/pm/business-case.md). Each decision below must obey
them.

1. **Do one thing.** Delegator controls the queue and the isolation. Other programs do
   all other work, and the person selects them.
2. **The person goes away and comes back.** The product is not a window on a run that
   operates now. Write it for a person who was away for 2 hours.
3. **The head of the person keeps no data.** The ticket file contains what to do and
   why. The agent adds a short `result` and short `flags`.
4. **Show less.** The person sees the ticket, the `result`, the `flags` and the
   variables. The person does not see the output of the agent.
5. **The inbox is one list.** All projects are in it. The person can apply a filter, but
   delegator does not apply one automatically.
6. **Closure is complete.** The value `flags none` shows directly that the ticket has no
   problem.
7. **The person keeps control.** The work is on a branch, in a worktree, and the person
   selects each external command.

## 3. What the prototype taught us

The table below gives each problem and its effect on this design. Some lessons from the
prototype are no longer applicable, because this version removes the code that caused
them. Section 4 gives that list.

| # | What occurred | Effect on the design |
|---|---|---|
| 1 | The MCP server operated inside the queue worker. When the queue stopped, the agent had no tools. | Delegator has no server. The agent calls the CLI. See §5. |
| 2 | The state `processing` had two meanings. A restart put chat work back into the queue, and started the first prompt again above live work. | A run is a first class entity, and it records its initiator. See §6.1. |
| 3 | `CREATE TABLE IF NOT EXISTS` does not add new columns. A new column gave the error `no such column` on each database that existed. | Delegator has no database. Each ticket file declares its `schema`. See §7. |
| 4 | The output of the agent went into the ticket. One ticket got 13378 characters. The short note for the person got 973 characters. | Delegator limits the length of `result` and `flags` when it writes them. A limit in a prompt does not operate. See §6.4. |
| 5 | The command `git difftool` started a graphical tool, and the TUI went away until the tool stopped. | Delegator has no diff and no built-in tools. The person configures each command. See §9.2. |
| 6 | The command `queue status` used the directory of the person as a filter, and hid tickets from other projects. | One inbox contains all projects. A filter is always explicit. See principle 5. |
| 7 | The command `queue down` released the lock while a run continued. A quick restart was able to start a second worker on the same worktree. | A file lock controls the queue. An operating system lock cannot become out of date. See §5. |
| 8 | Only the claude adapter operated. The other three came from documentation, and no one operated them. | Version 1 has claude only. The package `adapters/` keeps the seam for later work. See §6.6. |
| 9 | No limit controlled a run. A ticket stayed in `processing` with no timeout and no cancel command. | Each run has a timeout, a cancel command and a restart command. See §6.3. |
| 10 | Each test used a real agent run. The tests were slow, expensive and not repeatable. | A fake agent with a script is a first class test fixture. See §10.2. |

## 4. Scope

**In scope for version 1**

- Tickets as Markdown files, which the person can read and change with any editor.
- One queue for all projects, with an order that the person can change.
- Work in the background, in a worktree. One run at a time.
- The inbox, with the groups READY, RUNNING, QUEUED and DONE.
- A short `result` and short `flags` from the agent, with a limit on the length.
- The four variables for each ticket, for use by other programs.
- The option `--json` on each command that shows data.
- Commands of the person, which delegator starts in a new window.
- One installer for Linux and macOS.
- The claude agent.

**Not in scope for version 1.** These items are removed from the earlier draft, or they
belong to the parent product.

- A TUI. Version 1 is a CLI, and the TUI is the next milestone. See §9.1.
- More than one run at a time. See §6.2.
- An MCP server. The agent uses the CLI. See §5.
- A database, and therefore migrations of a database. See §7.
- Measurements of the person or of the runs.
- A diff, a difftool, or any other built-in external tool. See §9.2.
- The output of the agent in the inbox, and the code that gives it a structure.
- A limit on the count of tickets in READY. Tickets collect there, as mail does.
- A `.pkg` file for macOS, and a Homebrew formula. See §12.
- A command `dg upgrade`. The installer does this work.
- Agents other than claude.
- Work in the cloud, a web interface, more than one person, or teams.

## 5. Architecture

Delegator has no server and no long-life program. This is the largest change from the
earlier draft, and it removes lesson 1 and lesson 7 completely.

```
   dg (CLI)                         claude
      │                          (calls dg)
      └────────────┬──────────────────┘
                   ▼
           ┌───────────────┐
           │  queue lock   │   flock. One writer at a time.
           └───────┬───────┘
                   ▼
           ┌───────────────┐
           │ files on disk │   tickets, queue, config, logs
           └───────┬───────┘
                   ▼
           ┌───────────────┐
           │ dg run <id>   │   one supervisor for each run. It starts the
           │ (supervisor)  │   agent, applies the timeout, writes the state,
           └───────┬───────┘   starts the next ticket, and then stops.
                   ▼
             claude process ──► git worktree
```

**How the queue continues.** Each supervisor is a short program that operates apart from
its parent. When its run stops, the supervisor gets the lock, writes the state, and starts
the supervisor for the next ticket in the queue. The queue therefore continues after the
person closes the terminal.

**How the queue recovers.** A supervisor can stop with no report, from a crash or from a
restart of the computer. Each CLI command therefore does a reconcile below the lock. The
reconcile finds each run with no live program, marks it `failed`, and starts the next
ticket if no run is active.

**Why there is no long-life program.** Only two functions must continue after a command
stops: the run itself, and the start of the next run. A supervisor that operates apart
does both. A long-life program adds a life to control, and lesson 7 came from exactly
that.

**Why the agent uses the CLI.** Each agent can start a program. The CLI is therefore
available to every agent, and no configuration is necessary. This removes the server, the
port, the token and the life of the server.

## 6. Main decisions

### 6.1 A run is a first class entity

A ticket has runs. Each run records the type of work, which is `initial`, `restart` or
`revision`. It also records the initiator, which is `queue` or `person`. It records the
agent, the session, the worktree, the branch, the start time, the end time and the exit
code.

The reconcile in §5 asks which runs have no live program. No flag for an owner is
necessary, and a run from a chat is not a special condition.

### 6.2 One run at a time, and two orders

Version 1 operates one run at a time. This removes the control of slots, and it removes
each race between two supervisors. The config holds the value, and a later version can
raise it.

Two lists have two different orders. This section replaces §6.3 of the earlier draft,
which was not correct.

| List | Order | Why |
|---|---|---|
| QUEUED | The order of the queue. The person can change it. | The person controls what operates next. |
| READY | The time of completion. A new ticket goes at the end. | The list is stable. |

The earlier draft put READY in the order of the queue. That order is not stable. A slow
ticket that entered the queue first comes into the list **above** tickets that the person
can see now. The list therefore moves below the eyes of the person. An order by time of
completion only adds to the end, so no row moves.

There is no limit on the count of tickets in READY. Tickets collect there, as mail
collects in a mail inbox. The count can be 1 or 100.

### 6.3 Limits on a run, and the restart

Each run has a timeout. The default is 60 minutes, and the config holds the value. If the
run goes above the timeout, the supervisor stops the agent, and the state becomes
`failed`.

An error can also come from outside. An example is an API that does not reply. The command
`dg restart <id>` therefore starts the run again. It continues the same session, in the
same worktree, so the agent keeps the work that it did.

The command `dg cancel <id>` stops a run and keeps the worktree.

### 6.4 The summary from the agent: `result` and `flags`

The agent must end its work with this command:

```
dg finish <id> --result "..." --flags "..."
```

Delegator limits `result` to 160 characters and `flags` to 240 characters. It applies the
limit when it writes the file, because a limit in a prompt does not operate. The prototype
gives the data: the prompt asked for one line, and the agent gave 973 characters.

The field `flags` is not optional. The agent gives `none` if each item went as the ticket
said. A person who gives work to another person expects a report of each item that did not
go as expected. This field is that report, and `none` is a complete answer.

A run that stops before `dg finish` becomes `failed`, and not `ready`. A ticket that looks
complete but is not complete is the most expensive error, so delegator does not let it
occur.

The full data stays available, but away from the eyes of the person. The session has the
complete conversation. The commit messages on the branch have the detail. The log file has
the raw output.

### 6.5 The git boundary, worktrees and branches

Delegator accepts a ticket only if the directory is in a git repository. This condition
operates from version 1. It prevents work on files that no version control protects.

Each run gets one worktree, and one branch `delegator/<id>-<slug>`. The branch comes from
the default branch of the repository. Delegator removes the worktree at closure, but it
never removes the branch. The person can therefore open the branch later. The prototype
confirmed this behaviour.

### 6.6 The seam for other agents

The package `adapters/` holds one interface, and one implementation for claude. The
interface is small:

```go
type Adapter interface {
    Launch(spec RunSpec) *exec.Cmd   // headless run, with a session id that we give
    Resume(session string) []string  // argv for an interactive session
    Name() string
}
```

Delegator gives the session id to the agent, and does not read it from the output. The
command `claude --session-id <uuid>` accepts a UUID, and a test confirmed this behaviour.
No code therefore reads the output of an agent.

## 7. Data on disk

There is no database. Files are the data, and the person can read each one.

```
$XDG_CONFIG_HOME/delegator/config.toml

$XDG_DATA_HOME/delegator/
  queue                             ordered ticket ids, one on each line
  next-id                           the counter for ticket ids
  lock                              the file for the exclusive lock
  projects/
    web-api-4f2a91/
      project.toml                  the full path and the default branch
      tickets/
        0004-remove-staging/
          ticket.yaml             the fields. Delegator writes this file.
          ticket.md               the prose. The person writes this file.
      worktrees/
        0004-remove-staging/
      runs/
        0004/
          2026-08-17T09-30-00.log   the raw output of the agent
```

**The project key.** The key is the name of the directory of the repository, and 6
characters from a hash of its full path. An example is `web-api-4f2a91`. The name
alone is not unique, because many repositories have the name `backend`. The file
`project.toml` holds the full path, so the key is reversible.

**If the person moves a project.** The hash comes from the path, so a move gives a new
key. Two items break at the same time. Delegator loses the connection to its tickets, and
git loses the connection to each worktree. The `.git` file of a worktree holds the old
path of the repository.

Delegator therefore does a check. Each command reads `project.toml` of each project. If a
recorded path is not on the disk, delegator says so, and does not make a new project:

```
$ dg
delegator: the project at /home/person/projects/web-api is not on the disk.
           If you moved it, go to its new position and run `dg project relink`.
```

The command `dg project relink` does this work, from the new position:

1. It reads the first commit of the repository, and finds the project with the same first
   commit.
2. It changes the name of the project directory to the new key.
3. It writes the new path into `project.toml`, and into each ticket of that project.
4. It does `git worktree repair` for each worktree of that project.

Delegator does not do this work automatically. A copy of a repository has the same first
commit as its source, so two projects can look the same. A command from the person is
therefore necessary. A project with no open work is not affected, because a new key with
no tickets does no damage.

**Ticket ids are unique for all projects.** The file `next-id` holds the counter, and the
lock protects it. The command `dg show 4` is therefore not ambiguous, and the inbox can
show all projects together.

**The format of a ticket.** Each ticket is one directory with two files. The file
`ticket.yaml` holds the fields, and delegator writes it. The file `ticket.md` holds the
prose, and the person writes it.

```yaml
# ticket.yaml
schema: 1
id: 4
title: Remove staging infrastructure
state: ready
project: /home/person/projects/web-api
branch: delegator/4-remove-staging-infrastructure
worktree: ~/.local/share/delegator/projects/web-api-4f2a91/worktrees/0004-...
session: e55e382e-2c88-4de7-a31d-ab8763a0fb5a
result: Staging infra removed. Gate green, 433 tests, 100% branch coverage.
flags: terraform apply is blocked, the token in .env is invalid. Do not destroy
  the app first, because DNS points at it.
created: 2026-08-17T09:30:00Z
```

The file `ticket.md` holds the prose only:

```markdown
Remove the staging app, the volume, the DNS records, the monitor and the
secrets.
```

**Why there are two files.** Delegator writes `ticket.yaml` only. After it makes the
ticket, no command of delegator writes `ticket.md`. A person can therefore change the
prose with an editor at any time, and no command can damage the text. The two files also
make the code more simple. The file `ticket.yaml` is YAML, and the file `ticket.md` is
text. No program must find the end of a header, and the prose needs no escape characters.

**The title.** The command `dg ticket` with no arguments opens `$EDITOR`. The first line
becomes the field `title`, and the other lines become `ticket.md`. The title is a field,
and not the first line of the prose, because delegator makes the row of the inbox, the
name of the branch and the name of the directory from it.

**Upgrades.** The field `schema` is the complete answer to the problem of upgrades. A new
version of delegator reads each older schema, and writes the new schema. If a file
declares a schema that the program does not know, the program stops with an error. It does
not write the file. A person who installs an older version therefore loses no data.

## 8. States of a ticket

One module controls each change of state. An illegal change causes an error, and the
module does not write it.

```
   ┌──────────┐   dg revise                        ┌───────────┐
   │  queued  │◄──────────────────────────────────►│   ready   │
   └────┬─────┘                                    └─────┬─────┘
        │ a supervisor starts                            │ dg accept
        ▼                                                ▼
   ┌──────────┐   dg finish                        ┌──────────┐
   │ running  ├───────────────────────────────────►│   done   │
   └────┬─────┘                                    └──────────┘
        │ timeout, error, or no dg finish
        ▼
   ┌──────────┐   dg restart
   │  failed  ├──────────────► queued
   └──────────┘
```

The state `cancelled` comes from `queued`, `running` or `ready`.

## 9. Interfaces for the person

### 9.1 The inbox

The command `dg` gives the inbox. The groups are always in the same order, because
recognition is easier than memory.

```
$ dg
READY
  4  web-api  Remove staging infra   ⚠ terraform blocked, token invalid
  7  data-loader   Add rate limiting      none
RUNNING
  9  web-api  Migrate to Python 3.13   14m
QUEUED
 11  data-loader   Switch coverage to another tool
```

The `flags` of each ticket in READY are on the same row. The person can therefore see
which tickets have a problem, and can accept the other tickets with no more commands.

The command `dg show 4` gives one ticket in full:

```
$ dg show 4
  #4  Remove staging infrastructure                    ready · 2h ago
  ───────────────────────────────────────────────────────────────────
  flags     terraform apply is blocked, the token in .env is
            invalid. Do not destroy the app first, because DNS
            points at it.
  result    Staging infra removed. Gate green, 433 tests.

  ticket    …/projects/web-api-4f2a91/tickets/0004-remove-staging/
  worktree  …/projects/web-api-4f2a91/worktrees/0004-remove-staging
  branch    delegator/4-remove-staging-infrastructure
  session   e55e382e-2c88-4de7-a31d-ab8763a0fb5a

  Remove the staging app, the volume, the DNS records, the monitor
  and the secrets.
```

A ticket with no problem shows `flags     none`.

### 9.2 Commands of the person

Delegator starts no external tool of its own. The person declares each command in the
config, with the variables from §7:

```toml
terminal = "ptyxis --new-window -d {worktree} --"
timeout_minutes = 60
runs = 1

[commands.diff]
run = "git difftool -d {base}...HEAD"
window = true

[commands.chat]
run = "claude --resume {session}"
window = true

[commands.edit]
run = "$EDITOR {ticket}"
window = false
```

The variables are `{ticket}`, `{worktree}`, `{branch}`, `{base}`, `{session}` and
`{project}`. The variable `{ticket}` gives the path of `ticket.md`, because the person
changes the prose and not the fields. The command `dg open diff 4` starts the command with the name `diff` for
ticket 4. A command with `window = true` opens in a new terminal window.

This removes the detection of tools and of terminals from delegator. It also lets each
person keep the tools that they have now. When the TUI comes, each command also gets a
key.

### 9.3 The CLI

The command is `delegator`, and `dg` is a short name for it. Both names come from the
installer.

| Command | Function |
|---|---|
| `dg ticket [title] [body]` | Add a ticket. With no arguments, it opens `$EDITOR`. |
| `dg` | Show the inbox. |
| `dg show <id>` | Show one ticket and its variables. |
| `dg open <name> <id>` | Start a command of the person. See §9.2. |
| `dg start` and `dg pause` | Start or stop work on the queue. |
| `dg queue` | Open the queue file in `$EDITOR`, to change the order. |
| `dg restart <id>` | Start a failed run again. See §6.3. |
| `dg cancel <id>` | Stop a run. |
| `dg accept <id>` | Close a ticket, and remove its worktree. |
| `dg revise <id> <text>` | Put a ticket back in the queue, with more instructions. |
| `dg run <id>` | The supervisor. Delegator starts this, and a person does not. |
| `dg project relink` | Connect a project again after a move. See §7. |
| `dg doctor` | Do a check of git, of claude, of the config and of the permissions. |

Each command that shows data also accepts `--json`. A different interface, or a script of
the person, can therefore read the data and not the text. Section 12 shows why.

The agent uses two commands only:

| Command | Function |
|---|---|
| `dg read <id>` | Read the ticket, with each change that came after the start. |
| `dg finish <id> --result … --flags …` | End the work. See §6.4. |

## 10. How to prevent errors

You do not want a long sequence of new errors after each correction. This section shows
how to prevent them.

### 10.1 Types and limits

Go gives types at compilation. Each change of state is in one module, and an illegal
change causes an error. The limits on `result` and `flags` operate when delegator writes
the file, and not when it reads it.

### 10.2 The fake agent

A program `dg-fake-agent` reads a script, and does what the script says. It can write
files, call `dg finish`, stop with an error, stop with no output, and continue past the
timeout.

This decision has the largest effect in this document. It makes the queue, the supervisor,
the reconcile and the inbox testable. The tests are repeatable, they operate in seconds,
and they have no cost. In the prototype each of these parts used a real agent run, and
this is why the person found the errors.

### 10.3 Levels of tests

| Level | What it examines | Speed |
|---|---|---|
| Unit | States, the order of lists, the limits, the format of a ticket | milliseconds |
| Golden | The ticket file format, for each schema version | milliseconds |
| Integration | The CLI with the fake agent: complete lives, the reconcile after a crash, the queue order | seconds |
| Complete system | One real claude run | minutes. Marked. Not on each commit. |

For each error that a person reports, write a test at the lowest level that finds it. Give
the date and the symptom. Do this before the correction goes in.

## 11. Security

- Delegator has no server, no port and no token. There is no network interface.
- The agent operates with its permission questions off. The area of effect is the
  worktree, and all data that the agent can get to. A worktree gives isolation, but it is
  not a sandbox. This document says so directly, and version 1 does not pretend to have a
  sandbox.
- Delegator keeps no credentials. The agent keeps its own.
- Ticket files can contain private data. They stay in the data directory of the person,
  outside each git repository, so a commit cannot send them away.

## 12. Repository, tools and installation

```
github.com/alcubie/delegator
  cmd/
    dg/              the binary. It reads the arguments and calls internal/cli.
  internal/
    ticket/          the file format, the schema, read and write
    queue/           the order, the lock, the reconcile
    run/             the supervisor, the timeout, the worktree
    adapters/        the interface for an agent, and claude
    config/          the config and the commands of the person
    inbox/           the groups and their order. It gives data, and not text.
    cli/             the commands, and the text for a terminal
  test/              unit, golden, integration, complete system
  docs/              this document and the decision records
```

**Where a new interface goes.** The package `inbox/` gives a data structure. It does not
know which interface shows the data, and it writes no text. The package `cli/` makes the
text for a terminal from that structure. This boundary is the cause of two packages, and not
one.

A TUI therefore becomes `internal/tui/`. It reads the same structure from `inbox/`, and it
stays inside the binary `dg`, as the command `dg --watch`.

A GUI in Go becomes a second binary `cmd/dg-gui/`. A graphical library is large, and the
binary `dg` must stay small. A GUI in a different language uses `dg --json`, and no Go
code is necessary for it.

**Go.** Go gives one binary with no other program below it. The person installs no other
item. No Go library is necessary on the computer of the person, because the compiler puts
each library inside the binary. Go is also good at the control of programs that operate at
the same time, which is the main work of the supervisor.

**Installation.** goreleaser makes the binary for Linux and macOS, for x86-64 and for
arm64. One command installs it:

```
curl -fsSL https://alcubi.ai/delegator/install.sh | sh
```

The same command gets a new version later. Version 1 has no `.pkg` file and no Homebrew
formula, because this command operates on both Linux and macOS.

## 13. Milestones

Each milestone uses the fake agent, includes tests, and is usable at its end.

1. **Files and the queue.** The ticket format, the schema, the project key, the queue, the
   lock, the reconcile, and the commands `ticket`, `dg` and `show`. Also the check for a
   moved project, and `dg project relink`. No agent.
2. **Work.** The supervisor, the worktree, the git boundary, the timeout, `start`,
   `pause`, `cancel` and `restart`. The claude adapter. The commands `read` and `finish`,
   with their limits.
3. **Closure.** The commands `accept` and `revise`, the removal of a worktree, and the
   `flags` on each row of the inbox.
4. **Commands of the person.** The config, the variables, `dg open` and the new window.
5. **Installation.** goreleaser, the installer for Linux and macOS, and `dg doctor`.

Milestone 1 and milestone 2 have the longest duration. Each milestone after them adds to
the system.

The TUI is the first milestone after version 1. Section 9.2 keeps its place: each command
of the person also gets a key.

## 14. How we will work

You examine each commit while we build. Each commit is therefore small, and it does one
thing. Each commit message says what changed and why. A commit that corrects an error
that you reported also contains its test.

## 15. Names

The product is **delegator**, and its command is `dg`. The group is **Alcubi**.

| Item | Value |
|---|---|
| Brand and documentation | Alcubi |
| Canonical domain | `alcubi.ai` |
| Installer | `https://alcubi.ai/delegator/install.sh` |
| Repository | `github.com/alcubie/delegator` |
| Display name of the group on GitHub | Alcubi |
| Domains that go to the canonical domain | `alcubi.com`, `alcubie.com` |

The name `alcubi` was not available for a group on GitHub, so the slug in the URL is
`alcubie`. GitHub keeps the display name of a group apart from the slug, so the page reads
Alcubi. The same condition exists for flyctl: the installer comes from `fly.io`, and the
code is at `github.com/superfly/flyctl`.

The installation page must give a link to the repository. A person who sends a script from
`alcubi.ai` to a shell can then see that the two names belong to one group.

## 16. Questions for you

Each question from the earlier draft now has an answer:

- The project key uses the hash, and §7 gives the behaviour after a move.
- Ticket ids are one sequence for all projects.
- The limits are 160 characters for `result` and 240 for `flags`.
- The names are in §15.
