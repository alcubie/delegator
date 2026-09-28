# Delegator

[![Documentation checks](https://github.com/alcubie/delegator/actions/workflows/docs.yml/badge.svg)](https://github.com/alcubie/delegator/actions/workflows/docs.yml)
[![Documentation](https://img.shields.io/badge/docs-alcubi.ai-blue)](https://alcubi.ai/delegator/docs/)
[![License: FSL-1.1-ALv2](https://img.shields.io/badge/license-FSL--1.1--ALv2-blue)](LICENSE)

**Delegate coding tasks. Keep your focus.**

Delegator is a command-line tool for managing work across AI coding agents and
Git repositories. Queue up tasks, let agents work in separate branches and
worktrees, and return to one inbox of changes ready for review.

Spend less time juggling agent sessions and managing branches. Delegator
handles the queue and task dependencies so you can focus on understanding the
changes and approving the work.

[Visit the homepage for visual examples](https://alcubi.ai/delegator)

## Features

- **Queue work and get on with your day.** Tasks start automatically as
  capacity becomes available.
- **Keep projects organized.** Manage tasks across repositories from one
  queue and review inbox.
- **Keep changes separate.** Each task gets its own Git branch and worktree.
- **Run work in the right order.** Link dependent tasks so follow-up work
  waits until its prerequisites are accepted.
- **Use your preferred coding agent.** Connect agents through the Agent
  Client Protocol (ACP), with setup for tools including Claude Code and Codex.

## Getting started

```sh
curl -fsSL https://alcubi.ai/delegator/install.sh | sh
```

You'll need Git and an installed, authenticated coding agent. See the
[documentation](https://alcubi.ai/delegator/docs/) for setup and agent
configuration.

Set up Delegator, then create a task in an existing Git repository:

```sh
dg init
cd /path/to/your/repository
dg ticket "Add a health check" "Add a health-check endpoint and tests."
dg
```

Delegator starts the task automatically when capacity is available. Run `dg`
to check your inbox. Review and merge the completed changes, then accept the
ticket with `dg accept ID`.

## Documentation

- [Documentation and CLI reference](https://alcubi.ai/delegator/docs/)
- [Releases and changelog](https://github.com/alcubie/delegator/releases)
- [Report a bug or request a feature](https://github.com/alcubie/delegator/issues)

## License

Delegator is source available under the
[Functional Source License (FSL-1.1-ALv2)](LICENSE).

[Privacy](PRIVACY.md) · [Security](SECURITY.md) ·
[Third-party notices](THIRD_PARTY_NOTICES.md) · [Trademarks](TRADEMARKS.md)
