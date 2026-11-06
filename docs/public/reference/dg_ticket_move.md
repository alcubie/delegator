# Alcubi Delegator CLI Reference: dg ticket move

## dg ticket move

Reorder tickets in their queue or ready list.

### Synopsis

Move one or more tickets within QUEUED or READY. Every ticket must be in the same list, and they land together as one block in the order that the list already holds them. The destination may be up, down, top, bottom, or the ID of another ticket in the same list, which places the tickets immediately before it. Up and down take one ticket.

```
dg ticket move <id>... <where> [flags]
```

### Examples

```
  dg ticket move 42 top
  dg ticket move 42 down
  dg ticket move 42 57
  dg ticket move 42 57 93 bottom
  dg ticket move 42 57 93 18
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

* [dg ticket](dg_ticket.md)	 - Work with tickets.
