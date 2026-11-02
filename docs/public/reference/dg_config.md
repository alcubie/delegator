# Alcubi Delegator CLI Reference: dg config

## dg config

Show or change instance settings.

### Synopsis

Show all settings for the selected Delegator instance. Use the get and set subcommands to inspect or change one setting.

```
dg config [flags]
```

### Examples

```
  dg config
  dg config get runs
  dg config set runs 2
```

### Options

```
  -h, --help   help for config
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
* [dg config get](dg_config_get.md)	 - Show one instance setting.
* [dg config list](dg_config_list.md)	 - Show every supported instance setting.
* [dg config set](dg_config_set.md)	 - Change one instance setting.
