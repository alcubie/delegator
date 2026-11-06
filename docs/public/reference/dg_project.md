# Alcubi Delegator CLI Reference: dg project

## dg project

Work with the registered projects.

### Synopsis

Register a repository as a project, name a project, archive one to hide it from the lists, restore an archived one, and show the repositories that this Delegator instance has registered.

```
dg project [flags]
```

### Examples

```
  dg project add
  dg project list
  dg project rename Backend
  dg project archive
  dg project restore
```

### Options

```
  -h, --help   help for project
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
* [dg project add](dg_project_add.md)	 - Register a repository as a project.
* [dg project archive](dg_project_archive.md)	 - Archive a project so it leaves the lists.
* [dg project list](dg_project_list.md)	 - List every registered project.
* [dg project rename](dg_project_rename.md)	 - Give a project a name.
* [dg project restore](dg_project_restore.md)	 - Restore an archived project to the lists.
