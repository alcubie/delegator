# Alcubi Delegator CLI Reference: dg search

## dg search

Find tickets by their text

### Synopsis

Search ticket titles and prose for a case-insensitive text pattern. By default the search covers tickets in every project and every status.

```
dg search <pattern> [flags]
```

### Examples

```
  dg search "rate limit"
  dg search timeout --project ../api
```

### Options

```
  -h, --help             help for search
      --project string   search only the project in this directory (default: every project)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
