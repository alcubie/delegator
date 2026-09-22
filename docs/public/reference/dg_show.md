# Alcubi Delegator CLI Reference: dg show

## dg show

Show the details of a ticket.

### Synopsis

Show a ticket's status, project, worktree, branch, session, commit, dependency links, run history, and prose. With no ID, show the first ready ticket for the selected project.

```
dg show [id] [flags]
```

### Examples

```
  dg show 42
  dg show
  dg show 42 --worktree-only
```

### Options

```
      --branch-only      write only the branch of the work instead of the full ticket
  -h, --help             help for show
      --project string   select the project whose first ready ticket to show when ID is omitted (default: current working directory)
      --project-only     write only the directory of the project instead of the full ticket
      --session-only     write only the session of the last run instead of the full ticket
      --ticket-only      write only the file that holds the prose instead of the full ticket
      --worktree-only    write only the directory the run works in instead of the full ticket
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
