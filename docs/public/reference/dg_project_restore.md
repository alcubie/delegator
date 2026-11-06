# Alcubi Delegator CLI Reference: dg project restore

## dg project restore

Restore an archived project to the lists.

### Synopsis

Restore the project registered at dir, or at the current working directory, so that it returns to the lists of the desktop app. dir has to match the registered path of a project exactly: it is not resolved through Git, and it does not have to exist, so a project whose repository was deleted can still be restored. The tickets of the project are untouched. Restoring a project that is not archived changes nothing and is not an error.

```
dg project restore [dir] [flags]
```

### Examples

```
  dg project restore
  dg project restore ~/projects/widget
```

### Options

```
  -h, --help   help for restore
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg project](dg_project.md)	 - Work with the registered projects.
