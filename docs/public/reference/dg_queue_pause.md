# Alcubi Delegator CLI Reference: dg queue pause

## dg queue pause

Pause the queue.

### Synopsis

Pause the queue so no new agent runs start. Runs already in progress are allowed to finish.

```
dg queue pause [flags]
```

### Examples

```
  dg queue pause
```

### Options

```
  -h, --help   help for pause
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg queue](dg_queue.md)	 - Control the queue.
