# Alcubi Delegator CLI Reference: dg agents

## dg agents

List available Agent Client Protocol (ACP) commands

### Synopsis

List registered Agent Client Protocol (ACP) commands that are available on PATH, including their resolved executable, resume support, and which one is the default.

```
dg agents [flags]
```

### Examples

```
  dg agents
  dg agents --all
```

### Options

```
      --all    include registered agents whose executable is missing (default: show available agents only)
  -h, --help   help for agents
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
* [dg agents add](dg_agents_add.md)	 - Add or update an Agent Client Protocol (ACP) command
