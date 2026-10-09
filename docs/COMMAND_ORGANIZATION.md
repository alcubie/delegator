# Command organisation

Status: Delivered. Date: 2026-10-08.

Ticket operations are grouped under `dg ticket` and queue controls under `dg queue`. The canonical grammar is `dg ticket <action> [arguments]`, with a separate `dg ticket runs <id>` command for run history. `dg ticket create` is the only creation form and `ticket.create` is its only structured method. Other former root commands and methods remain available as compatibility entry points.

## Purpose and boundaries

The original command-line interface (CLI) mixed instance operations, ticket operations, and internal worker commands at the root. Grouping commands makes help easier to scan and gives new ticket features a predictable home. The desktop should request run history only when the user opens Runs.

This change concerns naming, routing, discovery, and help. It preserves existing scheduling, acceptance checks, dependency semantics, project selection, and return values. It does not change the database or introduce a persistent server. Existing reads can still perform normal Delegator reconciliation; a new command group does not make those reads observational only.

## Implementation

The command tree is assembled in [root.go](../internal/cli/root.go). `ticket` and `queue` are namespace commands whose children hold the canonical operations. Ticket creation uses `ticket create [title] [body]`; no arguments open an editor. Retained root commands use separate command instances with the same operation handlers. `config` and `agents` retain their existing subcommands.

The JavaScript Object Notation Remote Procedure Call (JSON-RPC) adapter derives dotted method names from command paths in [rpc.go](../internal/cli/rpc.go). For example, `config get` becomes `config.get`. Discovery walks that same tree in [openrpc.go](../internal/cli/openrpc.go), while result-schema selection and some execution restrictions use explicit names.

Operation identity now carries the name-based remote-call policy across canonical and compatibility commands. This keeps result schemas, creation requirements, and remote-call eligibility stable when one operation has two command paths.

## Command grammar

| Form | Assessment |
| --- | --- |
| `dg ticket accept 42` | Recommended. The action is a stable subcommand with its own help, arguments, flags, completion, and dotted method name. Collection operations use the same structure. |
| `dg ticket 42 accept` | Readable for one ticket, but needs identifier-aware routing outside the ordinary command tree. Collection operations such as create and list need a different grammar. |
| `dg accept 42` | Useful existing shorthand. Retain it as a compatibility entry point while documenting the grouped form. |

Support one canonical grouped grammar. Do not add both identifier-first and action-first forms.

## Proposed command map

Angle brackets denote required arguments; square brackets denote optional ones. Preserve the current identifier optionality and project-selection rules for existing operations. The new runs command requires an explicit identifier.

| Area | Canonical command | Treatment of existing entry point |
| --- | --- | --- |
| Inbox | `dg` | Unchanged; JSON-RPC method remains `inbox` |
| Create | `dg ticket create [title] [body]` | Replace `dg ticket [title] [body]`; no compatibility alias |
| List | `dg ticket list` | `dg list` |
| Search | `dg ticket search <pattern>` | `dg search <pattern>` |
| Inspect | `dg ticket show [id]` | `dg show [id]` |
| Dependency graph | `dg ticket map [id]` | `dg map [id]` |
| Edit | `dg ticket edit <id>` | `dg edit <id>` |
| Ordering | `dg ticket move <id> <where>` | `dg move <id> <where>` |
| Dependencies | `dg ticket depend <id>` | `dg depend <id>` |
| Record completion | `dg ticket finish <id> <commit>` | `dg finish <id> <commit>` |
| Accept | `dg ticket accept [id]` | `dg accept [id]` |
| Cancel | `dg ticket cancel <id>` | `dg cancel <id>` |
| Restart | `dg ticket restart <id>` | `dg restart <id>` |
| Conversation | `dg ticket chat [id]` | `dg chat [id]` |
| Run history | `dg ticket runs <id>` | New command |
| Queue control | `dg queue pause`, `dg queue start` | `dg pause`, `dg start` |
| Instance configuration | `dg config ...` | Unchanged |
| Agent configuration | `dg agents ...` | Unchanged |
| Setup and version | `dg init`, `dg version` | Unchanged |
| Protocol and help | `dg rpc`, `dg help`, completion commands | Unchanged |

`move` stays with tickets because it operates on a specific ticket, including tickets in Ready. Do not rename start to resume or depend to dependencies in this change; that would add another migration decision.

The hidden `run` supervisor and `telemetry-send` worker remain at their existing paths. Their launch sites and existing remote-call treatment are outside this reorganisation. In particular, `ticket runs` reads history; it does not start a run.

## Compatibility and ticket creation

### Explicit creation only

Creation requires `dg ticket create [title] [body]`. Bare `dg ticket` displays group help. Calling `dg ticket create` without arguments opens the editor, preserving the creation workflow under its explicit action.

Remove the legacy `dg ticket [title] [body]` creation route. The first word after `ticket` always names an action; an unknown action is an error. Do not detect title collisions, fall back to creation, or provide a special parent-level literal-title escape. A title such as `accept` is ordinary data after `ticket create`.

Normal end-of-options handling remains available on the create command for titles beginning with a dash. That is standard argument handling, not support for the removed creation form.

This is an intentional breaking change for creation callers. Update scripts, examples, agent instructions, completion, and remote clients to use the explicit creation operation. A creation compatibility period is not required.

### Existing commands and scripts

For operations other than creation, root-level entry points remain callable with their existing flags, defaults, output, exit behaviour, and validation. They share execution logic through command factories or operation functions rather than maintaining separate implementations.

Do not reparent the same mutable Cobra command instance in two places. Build separate command instances using shared handlers and independently bound flags. Creation-only flags must not become inherited flags on every ticket command.

Compatibility commands are deprecated and hidden from command listings, completion and generated reference pages. Invoking one prints a warning on standard error naming its replacement; direct help points to the replacement help. Remote discovery marks retained methods as deprecated and identifies their replacements. Structured result output remains unchanged. No removal date is proposed for those other compatibility commands. Creation is excluded: its old form and method were removed when explicit creation was introduced.

## Structured methods and discovery

Canonical methods follow the command tree: `ticket.create`, `ticket.show`, `ticket.accept`, `ticket.runs`, `queue.pause`, and `queue.start`.

The old `ticket` creation method was removed when `ticket.create` was introduced. The `ticket` parent is a namespace and is not advertised or callable as an operation. For other operations, existing method names (`show`, `accept`, `pause`, and so on) remain as compatibility methods. Shell command aliases alone do not provide this: the discovery walker derives names from each command's primary path.

For each retained old/new pair:

- Both names resolve to the same operation, argument semantics, and result schema.
- Discovery publishes both callable names during migration and identifies the preferred name in the method description.
- Interactive restrictions follow the operation. `ticket.chat` remains unavailable through JSON-RPC just as `chat` is today. Creation still requires an explicit project for remote callers. Editor and standard-input restrictions remain intact.
- Routing honours the selected method. Creation is invoked explicitly through `ticket.create`; the old `ticket` method returns method-not-found. Positional data must not redirect an already resolved request into a child command.
- Flags and literal arguments remain distinct through argument conversion. Test ordinary titles, leading dashes, spaces, and shell punctuation. Do not implement routing by assembling shell text.
- Namespace-only parents such as `queue` are not advertised as executable methods unless they have a defined operation and result.

Creation changed its method name while retaining its result shape and validation. Callers that create tickets must use `ticket.create`; the current desktop has no creation action. Other grouped names are additive under the compatibility policy, so their consumers can migrate independently.

## Separate run history command

### Request and response

`dg ticket runs <id>` lists every recorded run for that ticket, newest first by run identifier, using the store query delivered by ticket #378. Its JSON-RPC method is `ticket.runs` with exactly one positive ticket identifier in `params.args`.

Published response envelope:

```json
{
  "ticket_id": 42,
  "runs": [
    {
      "id": 105,
      "agent": "codex",
      "model": null,
      "started": "2026-10-08T16:00:00Z",
      "ended": null,
      "exit_code": null
    }
  ]
}
```

The envelope identifies the ticket even when `runs` is empty and allows future additive metadata. Require every listed field in the published result schema. Identifiers are positive integers. Agent/model are strings or null. Started/ended are date-time strings or null for unavailable recorded times. Exit code is an integer or null. Unknown fields remain permitted for additive compatibility.

An existing ticket with no runs returns an empty array. An unknown ticket returns an error. Preserve nulls: absence of an exit code does not prove success, and an absent end time alone is not a new persisted run status. Do not reconstruct per-run commits, sessions, or worktrees from current ticket fields.

Terminal output shows the same recorded history in a compact table, with an explicit empty-history message and readable missing values. Discovery publishes the result schema, and `make docs` generates the command reference.

### Delivered runs command and creation follow-up

Ticket #380 delivered `ticket runs <id>` independently, using the store query from #378. It landed beneath the existing creation parent before the decision to remove legacy creation support. The run-history operation and schema remain as implemented.

Ticket #382 made `ticket` a namespace containing `create`, `runs`, and the other grouped operations. It removed parent creation dispatch and the routing safeguards and tests retained solely for legacy creation while preserving general argument validation and exact-method routing.

The original #379 proposal embedded history in `show`; the separate runs command superseded that approach.

### Desktop loading

Opening Overview requests `show`; it does not request run history. On the first visit to Runs, fetch `ticket.runs` for the current ticket and connection. Show a loading state, a retryable error, or a confirmed empty-history state as appropriate.

Cache by connection and ticket identifier. Returning to Runs can reuse the result until refresh invalidates it. If Runs is active, manual refresh reloads its history. Otherwise, refresh marks that history stale and the next visit reloads it. Future automatic refresh should request history only for the active Runs tab. Clear caches on connection changes and reject late responses for a different selection or connection. Future ticket actions invalidate affected history after success.

Dependencies remain in inbox/show because those views need them immediately. Separating runs does not move dependency retrieval behind another tab or add a request per inbox row.

## Delivery

The command organisation was delivered in independently reviewable stages:

1. #380 added the separate runs command and schema.
2. #381 made remote routing follow operation identity so compatibility commands could share policy safely.
3. #382 introduced explicit creation, made bare `ticket` show help, and removed the legacy creation command and method.
4. #383 through #387 added grouped inspection, editing, scheduling, lifecycle, and conversation commands while retaining the compatible root entry points.
5. #388 added the queue group while retaining the compatible root start and pause commands.
6. #389 made help, completion, examples, discovery, and first-party callers consistently favour canonical names.

The desktop run-history work described below remains outside this command-line delivery.

## Verification requirements

- Retained old/new command parity for flags, project defaults, output, errors, and state transitions.
- Explicit creation, editor invocation through `ticket create`, group help, and invalid subcommand arguments. Verify the removed creation form does not create tickets.
- Exact remote-method routing: creation data cannot choose a different operation.
- Discovery includes the right result schema and argument bounds for every callable method; namespace parents and interactive commands remain correctly excluded.
- Explicit project requirements and editor restrictions still apply to canonical creation/editing.
- Runs covers no history, multiple restarts, equal start timestamps, missing metadata, active/ended runs, and unknown tickets.
- Desktop Runs requests are lazy, survive normal tab switching, invalidate correctly, and ignore stale responses.
- Generated reference and shell completion reflect the canonical structure while excluding deprecated compatibility forms.

## Delivered decisions

| Decision | Recommended choice |
| --- | --- |
| Ticket grammar | Action before identifier |
| Creation name | `ticket create` |
| Bare `dg ticket` | Show group help |
| Creation compatibility | None; remove the old command form and `ticket` method |
| Queue controls | `queue start` and `queue pause` |
| Other existing commands and remote methods | Retain as compatibility entry points |
| Run-history retrieval | Separate `ticket.runs`, fetched when Runs is opened |
| Broader implementation planning | Delivered in #380 through #389 |
