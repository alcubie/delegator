# Alcubi Delegator CLI Reference: dg ticket

## dg ticket

Work with tickets.

### Synopsis

Create and inspect tickets, their dependencies, and their recorded runs.

```
dg ticket [flags]
```

### Examples

```
  dg ticket create "Add request tracing" --no-body
  dg ticket list
  dg ticket show 42
  dg ticket runs 42
```

### Options

```
  -h, --help   help for ticket
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
* [dg ticket create](dg_ticket_create.md)	 - Add a ticket to a project queue.
* [dg ticket list](dg_ticket_list.md)	 - List every ticket, whatever its status
* [dg ticket map](dg_ticket_map.md)	 - Show the dependency graph of a ticket
* [dg ticket runs](dg_ticket_runs.md)	 - Show the recorded runs of a ticket.
* [dg ticket search](dg_ticket_search.md)	 - Find tickets by their text
* [dg ticket show](dg_ticket_show.md)	 - Show the details of a ticket.
