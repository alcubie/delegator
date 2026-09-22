# Alcubi Delegator CLI Reference: dg accept

## dg accept

Close a ready ticket.

### Synopsis

Accept a ready ticket after its branch has been merged, mark it done, remove its worktree, and start queued work if capacity is available. With no ID, accept the first ready ticket for the selected project.

```
dg accept [id] [flags]
```

### Examples

```
  dg accept 42
  dg accept
  dg accept 42 --force
```

### Options

```
      --force            skip the merge and clean-worktree checks; removing a dirty worktree loses its uncommitted changes
  -h, --help             help for accept
      --project string   select the project whose first ready ticket to accept when ID is omitted (default: current working directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
