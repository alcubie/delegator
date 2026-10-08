# Alcubi Delegator CLI Reference: dg ticket edit

## dg ticket edit

Change the title and the prose of a queued ticket.

### Synopsis

Change the title or prose of a queued ticket. Supply one or more text flags, or use --editor to edit both fields in $EDITOR.

```
dg ticket edit <id> [flags]
```

### Examples

```
  dg ticket edit 42 --title "Handle expired sessions"
  dg ticket edit 42 --body-file revised-plan.md
  dg ticket edit 42 --editor
```

### Options

```
      --body string        replace the ticket prose with this value
      --body-file string   read the replacement prose from this file, or from standard input when the value is -
      --editor             open $EDITOR to replace the title and prose instead of taking text flags
  -h, --help               help for edit
      --title string       replace the ticket title with this value
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg ticket](dg_ticket.md)	 - Work with tickets.
