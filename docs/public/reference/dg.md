# Alcubi Delegator CLI Reference

## dg

Delegate tasks to coding agents

### Synopsis

Delegator queues work for coding agents and keeps each task in its own Git worktree. Run dg without a command to see the inbox and the state of the queue.

```
dg [flags]
```

### Examples

```
  dg
  dg ticket create "Add request tracing" --no-body
  dg show 42
```

### Options

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
  -h, --help              help for dg
```

### SEE ALSO

* [dg accept](dg_accept.md)	 - Close a ready ticket.
* [dg agents](dg_agents.md)	 - List available Agent Client Protocol (ACP) commands
* [dg cancel](dg_cancel.md)	 - Stop the work on a ticket and close it.
* [dg chat](dg_chat.md)	 - Continue a ticket's agent session.
* [dg config](dg_config.md)	 - Show or change instance settings.
* [dg depend](dg_depend.md)	 - Add or remove dependencies of a queued ticket.
* [dg edit](dg_edit.md)	 - Change the title and the prose of a queued ticket.
* [dg finish](dg_finish.md)	 - Record the commit of a ticket and mark it Ready.
* [dg init](dg_init.md)	 - Set up Alcubi Delegator for a first run.
* [dg list](dg_list.md)	 - List every ticket, whatever its status
* [dg map](dg_map.md)	 - Show the dependency graph of a ticket
* [dg move](dg_move.md)	 - Reorder a ticket in its queue or ready list.
* [dg pause](dg_pause.md)	 - Pause the queue.
* [dg restart](dg_restart.md)	 - Start a failed ticket again.
* [dg rpc](dg_rpc.md)	 - Run a dg command from a JSON-RPC request on standard input.
* [dg search](dg_search.md)	 - Find tickets by their text
* [dg show](dg_show.md)	 - Show the details of a ticket.
* [dg start](dg_start.md)	 - Start a paused queue.
* [dg ticket](dg_ticket.md)	 - Work with tickets.
* [dg version](dg_version.md)	 - Show the version of dg.
