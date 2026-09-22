# Alcubi Delegator CLI Reference: dg cancel

## dg cancel

Stop the work on a ticket and close it.

### Synopsis

Stop a ticket's running agent when necessary and mark the ticket cancelled. The ticket's worktree is kept so uncommitted work can still be inspected.

```
dg cancel <id> [flags]
```

### Examples

```
  dg cancel 42
```

### Options

```
  -h, --help   help for cancel
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
