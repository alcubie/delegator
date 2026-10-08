# Alcubi Delegator CLI Reference: dg ticket runs

## dg ticket runs

Show the recorded runs of a ticket.

### Synopsis

Show every recorded run of one ticket, newest first. Missing recorded values are shown as dashes in terminal output and null in structured output.

```
dg ticket runs <id> [flags]
```

### Examples

```
  dg ticket runs 42
```

### Options

```
  -h, --help   help for runs
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg ticket](dg_ticket.md)	 - Work with tickets.
