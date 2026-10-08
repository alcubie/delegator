# Alcubi Delegator CLI Reference: dg ticket create

## dg ticket create

Add a ticket to a project queue.

### Synopsis

Add a ticket to a project's queue. Supply a title and prose, read the prose from --body-file, explicitly choose --no-body, or give no arguments to compose both fields in $EDITOR.

```
dg ticket create [title] [body] [flags]
```

### Examples

```
  dg ticket create "Remove the legacy endpoint" "Delete the handler and its tests."
  dg ticket create "Investigate the flaky test" --no-body
  dg ticket create "Implement the approved design" --body-file plan.md --after 41
  dg ticket create -- "-leading-dash" "Describe the change."
```

### Options

```
      --after int64Slice   make the new ticket depend on this ticket ID (may be repeated or comma-separated) (default [])
      --body-file string   read the ticket prose from this file, or from standard input when the value is -
  -h, --help               help for create
      --no-body            add the ticket with no prose instead of requiring a body
      --project string     add the ticket to the project in this directory (default: current working directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg ticket](dg_ticket.md)	 - Work with tickets.
