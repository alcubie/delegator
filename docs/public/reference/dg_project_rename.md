# Alcubi Delegator CLI Reference: dg project rename

## dg project rename

Give a project a name.

### Synopsis

Give the project registered at a directory a name of its own, for an instance where two projects share the folder name of their paths. Two projects may hold the same name. The directory matches the registered path of a project exactly, and need not exist, so a project whose repository moved or was deleted can still be renamed.

```
dg project rename [name] [flags]
```

### Examples

```
  dg project rename Backend
  dg project rename "Sleep backend" --project ~/projects/sleep/backend
  dg project rename --clear
```

### Options

```
      --clear            remove the given name, so the project uses the folder name of its path again
  -h, --help             help for rename
      --project string   rename the project registered at this directory (default: the current directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg project](dg_project.md)	 - Work with the registered projects.
