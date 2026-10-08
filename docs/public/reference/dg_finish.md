# Alcubi Delegator CLI Reference: dg finish

## dg finish

Record the commit of a ticket and mark it Ready. (use dg ticket finish)

### Synopsis

Record the ticket branch commit that contains the completed work and mark the ticket ready for review. The commit must belong to the ticket's branch. The preferred command is `dg ticket finish`; the preferred JSON-RPC method is `ticket.finish`.

```
dg finish <id> <commit> [flags]
```

### Examples

```
  dg finish 42 4f3c2b1
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

* [dg](dg.md)	 - Delegate tasks to coding agents
