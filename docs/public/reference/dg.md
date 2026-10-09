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
  dg ticket show 42
```

### Options

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
  -h, --help              help for dg
```

### SEE ALSO

* [dg agents](dg_agents.md)	 - List available Agent Client Protocol (ACP) commands
* [dg config](dg_config.md)	 - Show or change instance settings.
* [dg init](dg_init.md)	 - Set up Alcubi Delegator for a first run.
* [dg queue](dg_queue.md)	 - Control the queue.
* [dg rpc](dg_rpc.md)	 - Run a dg command from a JSON-RPC request on standard input.
* [dg ticket](dg_ticket.md)	 - Work with tickets.
* [dg version](dg_version.md)	 - Show the version of dg.
