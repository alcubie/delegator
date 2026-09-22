# Alcubi Delegator CLI Reference: dg move

## dg move

Reorder a ticket in its queue or ready list.

### Synopsis

Move a ticket within QUEUED or READY. The destination may be up, down, top, bottom, or the ID of another ticket in the same list, which places the ticket immediately before it.

```
dg move <id> <where> [flags]
```

### Examples

```
  dg move 42 top
  dg move 42 down
  dg move 42 57
```

### Options

```
  -h, --help   help for move
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
