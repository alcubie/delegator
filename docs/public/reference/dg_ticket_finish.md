# Alcubi Delegator CLI Reference: dg ticket finish

## dg ticket finish

Record the commit of a ticket and mark it Ready.

### Synopsis

Record the ticket branch commit that contains the completed work and mark the ticket ready for review. The commit must belong to the ticket's branch.

```
dg ticket finish <id> <commit> [flags]
```

### Examples

```
  dg ticket finish 42 4f3c2b1
```

### Options

```
  -h, --help   help for finish
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg ticket](dg_ticket.md)	 - Work with tickets.
