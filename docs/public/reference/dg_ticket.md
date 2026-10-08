# Alcubi Delegator CLI Reference: dg ticket

## dg ticket

Work with tickets.

### Synopsis

Create, inspect, and edit tickets, their dependencies, and their recorded runs.

```
dg ticket [flags]
```

### Examples

```
  dg ticket create "Add request tracing" --no-body
  dg ticket list
  dg ticket show 42
  dg ticket edit 42 --title "Handle expired sessions"
  dg ticket move 42 top
  dg ticket depend 42 --after 17
  dg ticket chat 42
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
* [dg ticket accept](dg_ticket_accept.md)	 - Close a ready ticket.
* [dg ticket cancel](dg_ticket_cancel.md)	 - Stop the work on a ticket and close it.
* [dg ticket chat](dg_ticket_chat.md)	 - Continue a ticket's agent session.
* [dg ticket create](dg_ticket_create.md)	 - Add a ticket to a project queue.
* [dg ticket depend](dg_ticket_depend.md)	 - Add or remove dependencies of a queued ticket.
* [dg ticket edit](dg_ticket_edit.md)	 - Change the title and the prose of a queued ticket.
* [dg ticket finish](dg_ticket_finish.md)	 - Record the commit of a ticket and mark it Ready.
* [dg ticket list](dg_ticket_list.md)	 - List every ticket, whatever its status
* [dg ticket map](dg_ticket_map.md)	 - Show the dependency graph of a ticket
* [dg ticket move](dg_ticket_move.md)	 - Reorder a ticket in its queue or ready list.
* [dg ticket restart](dg_ticket_restart.md)	 - Start a failed ticket again.
* [dg ticket runs](dg_ticket_runs.md)	 - Show the recorded runs of a ticket.
* [dg ticket search](dg_ticket_search.md)	 - Find tickets by their text
* [dg ticket show](dg_ticket_show.md)	 - Show the details of a ticket.
