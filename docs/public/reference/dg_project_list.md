# Alcubi Delegator CLI Reference: dg project list

## dg project list

List every registered project.

### Synopsis

List every project that this Delegator instance has registered, in alphabetical order of directory, with its name, the branch its worktrees start from, and a mark on an archived project. A project without a given name takes the name of its folder.

```
dg project list [flags]
```

### Examples

```
  dg project list
```

### Options

```
  -h, --help   help for list
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg project](dg_project.md)	 - Work with the registered projects.
