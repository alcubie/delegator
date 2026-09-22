# Alcubi Delegator CLI Reference: dg list

## dg list

List every ticket, whatever its status

### Synopsis

List tickets in every status and every project. Use --project to limit the list to the repository at a particular directory.

```
dg list [flags]
```

### Examples

```
  dg list
  dg list --project ../api
```

### Options

```
  -h, --help             help for list
      --project string   list only the project in this directory (default: every project)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
