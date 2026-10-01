# Alcubi Delegator CLI Reference: dg init

## dg init

Set up Alcubi Delegator for a first run.

### Synopsis

Discover installed agents, select or configure the default agent, and explain how to view the inbox and create the first ticket. Interactive setup asks whether to share telemetry if unanswered. Yes is selected, but only confirmation enables it; skip or cancel leaves it unanswered. Scripted setup preserves consent unless --telemetry=true|false is supplied.

```
dg init [flags]
```

### Examples

```
  dg init
  dg init --agent codex
```

### Options

```
      --agent string           select this available registered agent as the default without prompting
  -h, --help                   help for init
      --telemetry true|false   submit a telemetry choice (true|false)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
