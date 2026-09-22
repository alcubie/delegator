# Alcubi Delegator CLI Reference: dg chat

## dg chat

Continue a ticket's agent session.

### Synopsis

Continue a ticket's existing agent session in its worktree and wait for the interactive command. With no ID, continue the first ready ticket for the selected project.

```
dg chat [id] [flags]
```

### Examples

```
  dg chat 42
  dg chat
  dg chat --project ../api
```

### Options

```
  -h, --help             help for chat
      --project string   select the project whose first ready ticket to continue when ID is omitted (default: current working directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
