# Alcubi Delegator CLI Reference: dg project archive

## dg project archive

Archive a project so it leaves the lists.

### Synopsis

Archive the project registered at dir, or at the current working directory, so that it leaves the lists of the desktop app without being deleted from Delegator. dir has to match the registered path of a project exactly: it is not resolved through Git, and it does not have to exist, so a project whose repository was deleted can still be archived. The tickets of the project are untouched, and an open ticket keeps running and stays in the inbox. Archiving an archived project keeps the time of its first archive.

```
dg project archive [dir] [flags]
```

### Examples

```
  dg project archive
  dg project archive ~/projects/widget
```

### Options

```
  -h, --help   help for archive
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg project](dg_project.md)	 - Work with the registered projects.
