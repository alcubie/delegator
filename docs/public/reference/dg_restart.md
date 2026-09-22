# Alcubi Delegator CLI Reference: dg restart

## dg restart

Start a failed ticket again.

### Synopsis

Return a failed ticket to execution, reusing its branch, worktree, and agent session so work can continue where the failed run stopped.

```
dg restart <id> [flags]
```

### Examples

```
  dg restart 42
```

### Options

```
  -h, --help   help for restart
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
