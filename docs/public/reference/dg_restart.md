# Alcubi Delegator CLI Reference: dg restart

## dg restart

Start a failed ticket again. (use dg ticket restart)

### Synopsis

Return a failed ticket to execution, reusing its branch, worktree, and agent session so work can continue where the failed run stopped. A saved session uses its previous agent and recorded model by default, even if the defaults have changed. If the model is unknown, the loaded session's selection is left unchanged. If a recorded model is unavailable, the run fails before prompting. Use --model to select a model for this run instead. The preferred command is `dg ticket restart`; the preferred JSON-RPC method is `ticket.restart`.

```
dg restart <id> [flags]
```

### Examples

```
  dg restart 42
  dg restart 42 --model model-v2
```

### Options

```
  -h, --help           help for restart
      --model string   model ID to use for the new run
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
