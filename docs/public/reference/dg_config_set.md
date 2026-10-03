# Alcubi Delegator CLI Reference: dg config set

## dg config set

Change one instance setting.

### Synopsis

Validate and store a new value for one instance setting. The change applies to subsequent commands and agent runs.

```
dg config set <name> <value> [flags]
```

### Examples

```
  dg config set runs 2
  dg config set default_agent codex
  dg config set telemetry false
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg config](dg_config.md)	 - Show or change instance settings.
