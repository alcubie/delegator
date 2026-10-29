# Alcubi Delegator CLI Reference: dg project add

## dg project add

Register a repository as a project.

### Synopsis

Register the Git repository at dir, or the current working directory, as a Delegator project, without adding a ticket to its queue. A repository that is already registered, including from a subdirectory or a linked worktree, returns the project already on record.

```
dg project add [dir] [flags]
```

### Examples

```
  dg project add
  dg project add ~/projects/widget
```

### Options

```
  -h, --help   help for add
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg project](dg_project.md)	 - Work with the registered projects.
