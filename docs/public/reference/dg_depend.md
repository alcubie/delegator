# Alcubi Delegator CLI Reference: dg depend

## dg depend

Add or remove dependencies of a queued ticket.

### Synopsis

Make a queued ticket wait for one or more other tickets to be done. Use --remove to remove the named dependency links instead.

```
dg depend <id> [flags]
```

### Examples

```
  dg depend 42 --after 17
  dg depend 42 --after 17 --after 23
  dg depend 42 --after 17 --remove
```

### Options

```
      --after int64Slice   the ticket ID to add or remove as a dependency (required; may be repeated or comma-separated) (default [])
  -h, --help               help for depend
      --remove             remove the named dependency links instead of adding them
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
