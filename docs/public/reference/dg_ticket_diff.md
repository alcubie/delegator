# Alcubi Delegator CLI Reference: dg ticket diff

## dg ticket diff

Open the change of a ticket in your diff tool.

### Synopsis

Open the change of a ticket, from the default branch of its project to the work of its branch, and wait for the tool. Git starts the tool that diff.tool names, and Delegator holds no setting of its own for it. The exit status is the exit status of Git.

With no ID, open the change of the first ready ticket for the selected project.

```
dg ticket diff [id] [flags]
```

### Examples

```
  dg ticket diff 42
  dg ticket diff
  dg ticket diff --project ../api
```

### Options

```
  -h, --help             help for diff
      --project string   select the project whose first ready ticket to open when ID is omitted (default: current working directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg ticket](dg_ticket.md)	 - Work with tickets.
