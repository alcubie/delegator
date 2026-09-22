# Alcubi Delegator CLI Reference: dg map

## dg map

Show the dependency graph of a ticket

### Synopsis

Show the connected dependency graph containing a ticket. With no ID, start from the first ready ticket for the selected project.

```
dg map [id] [flags]
```

### Examples

```
  dg map 42
  dg map
  dg map 42 --mermaid
```

### Options

```
  -h, --help             help for map
      --mermaid          write a Mermaid flowchart instead of the default text tree
      --project string   select the project whose first ready ticket to map when ID is omitted (default: current working directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
