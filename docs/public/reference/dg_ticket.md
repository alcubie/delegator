# Alcubi Delegator CLI Reference: dg ticket

## dg ticket

Add a ticket to a project queue.

### Synopsis

Add a ticket to a project's queue. Supply a title and prose, read the prose from --body-file, explicitly choose --no-body, or give no arguments to compose both fields in $EDITOR. The first positional word runs is reserved; use dg ticket -- runs ... to create that title literally.

```
dg ticket [title] [body] [flags]
```

### Examples

```
  dg ticket "Remove the legacy endpoint" "Delete the handler and its tests."
  dg ticket "Investigate the flaky test" --no-body
  dg ticket "Implement the approved design" --body-file plan.md --after 41
  dg ticket -- "runs" "Describe a run-related change."
```

### Options

```
      --after int64Slice   make the new ticket depend on this ticket ID (may be repeated or comma-separated) (default [])
      --body-file string   read the ticket prose from this file, or from standard input when the value is -
  -h, --help               help for ticket
      --no-body            add the ticket with no prose instead of requiring a body
      --project string     add the ticket to the project in this directory (default: current working directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
* [dg ticket runs](dg_ticket_runs.md)	 - Show the recorded runs of a ticket.
