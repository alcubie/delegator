# Repository instructions

## Tickets

- Use `dg show <id>` to view an existing ticket. Do not use `dg ticket` for viewing; `dg ticket` creates a new ticket.
- When breaking work into tickets, use `dg ticket` to create them and supply `--project` when the current working directory is not the target project directory.
- Use `--after` or `dg depend` to link dependencies so work is processed in the intended order.

## Documentation

- Keep the README to only necessary information. Put detailed installation, configuration, and usage guidance in `docs/`.
- When changing CLI commands or flags, run `make docs` to regenerate the checked-in command reference.
- Write command descriptions to describe the command generally. Avoid details about individual settings, flags, or special cases unless they are necessary to understand the command.

## Documented commands

- Move multi-line shell commands in documentation into a Makefile target (or a script called by that target).
- Document a single `make` invocation instead of an inline multi-line shell block. Accept arguments when the command needs values such as a release version.
