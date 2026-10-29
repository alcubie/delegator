# Alcubi Delegator CLI Reference: dg ticket chat

## dg ticket chat

Continue a ticket's agent session.

### Synopsis

Continue a ticket's existing agent session in its worktree and wait for the interactive command. With no ID, continue the first ready ticket for the selected project.

Moving the session to the background from within chat may cause issues, and is best avoided. For example, Claude Code's /background command ends the session and starts a new process. This causes Delegator to show that the ticket has failed while it is still actually running. However, when the run has completed successfully, the ticket will move to the Ready bucket.

```
dg ticket chat [id] [flags]
```

### Examples

```
  dg ticket chat 42
  dg ticket chat
  dg ticket chat --project ../api
```

### Options

```
  -h, --help             help for chat
      --project string   select the project whose first ready ticket to continue when ID is omitted (default: current working directory)
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg ticket](dg_ticket.md)	 - Work with tickets.
