# Repository instructions

## Tickets

- Use `dg show <id>` to view an existing ticket. Do not use `dg ticket` for viewing; `dg ticket` creates a new ticket.
- When breaking work into tickets, use `dg ticket` to create them and supply `--project` when the current working directory is not the target project directory.
- Use `--after` or `dg depend` to link dependencies so work is processed in the intended order.
