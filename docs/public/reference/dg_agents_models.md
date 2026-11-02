# Alcubi Delegator CLI Reference: dg agents models

## dg agents models

List the models an agent offers

### Synopsis

Start one registered Agent Client Protocol (ACP) command, list the model IDs it advertises, and stop it. Those IDs are the values that default_model accepts.

```
dg agents models <name> [flags]
```

### Examples

```
  dg agents models claude
```

### Options

```
  -h, --help   help for models
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg agents](dg_agents.md)	 - List available Agent Client Protocol (ACP) commands
