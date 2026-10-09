# Alcubi Delegator CLI Reference: dg queue

## dg queue

Control the queue.

### Synopsis

Pause or start work across the queue.

```
dg queue [flags]
```

### Examples

```
  dg queue pause
  dg queue start
```

### Options

```
  -h, --help   help for queue
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
* [dg queue pause](dg_queue_pause.md)	 - Pause the queue.
* [dg queue start](dg_queue_start.md)	 - Start a paused queue.
