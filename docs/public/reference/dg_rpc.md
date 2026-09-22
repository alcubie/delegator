# Alcubi Delegator CLI Reference: dg rpc

## dg rpc

Run a dg command from a JSON-RPC request on standard input.

### Synopsis

Read one JSON-RPC 2.0 request or batch from standard input, run the named dg method, and write a JSON-RPC response to standard output.

```
dg rpc [flags]
```

### Examples

```
  printf '%s\n' '{"jsonrpc":"2.0","method":"version","id":1}' | dg rpc
  printf '%s\n' '{"jsonrpc":"2.0","method":"show","params":{"args":[42]},"id":1}' | dg rpc
```

### Options

```
  -h, --help   help for rpc
```

### Options inherited from parent commands

```
      --color mode        when to colour status values: always, never, or auto (terminals only) (default auto)
      --data-dir string   store all Delegator data in this absolute directory (default: the platform data directory)
```

### SEE ALSO

* [dg](dg.md)	 - Delegate tasks to coding agents
