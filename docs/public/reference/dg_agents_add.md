# Alcubi Delegator CLI Reference: dg agents add

## dg agents add

Add or update an ACP agent command

### Synopsis

Register a custom agent command, or set the executable path of a known agent. Repeated --arg values define the arguments passed to a custom command.

```
dg agents add <name> [flags]
```

### Examples

```
  dg agents add codex --path /opt/bin/codex-acp
  dg agents add local --command /opt/bin/local-agent --arg serve --arg=--acp
```

### Options

```
      --arg stringArray   append this argument to the custom command in order (may be repeated)
      --command string    register this executable as a custom agent command (mutually exclusive with --path)
  -h, --help              help for add
      --path string       set this executable path for a known agent (mutually exclusive with --command)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg agents](dg_agents.md)	 - List available ACP agents
