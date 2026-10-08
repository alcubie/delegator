# Command organisation

Status: Draft for review. Date: 2026-10-08.

Group ticket operations under `dg ticket` and queue controls under `dg queue`. Use `dg ticket <action> [arguments]`, with a separate `dg ticket runs <id>` command for run history. Keep existing commands and structured method names during migration. The broader reorganisation awaits review; only the separate runs command is currently requested for implementation.

## Purpose and boundaries

The current command-line interface (CLI) mixes instance operations, ticket operations, and internal worker commands at the root. Grouping commands makes help easier to scan and gives new ticket features a predictable home. The desktop should request run history only when the user opens Runs.

This change concerns naming, routing, discovery, and help. It preserves existing scheduling, acceptance checks, dependency semantics, project selection, and return values. It does not change the database or introduce a persistent server. Existing reads can still perform normal Delegator reconciliation; a new command group does not make those reads observational only.

## Current implementation

The command tree is assembled in [root.go](../internal/cli/root.go). Ticket creation currently uses `ticket [title] [body]`; no arguments open an editor. Most other ticket commands are root commands. `config` and `agents` already have subcommands.

The JavaScript Object Notation Remote Procedure Call (JSON-RPC) adapter derives dotted method names from command paths in [rpc.go](../internal/cli/rpc.go). For example, `config get` becomes `config.get`. Discovery walks that same tree in [openrpc.go](../internal/cli/openrpc.go), while result-schema selection and some execution restrictions use explicit names.

Those existing name-based rules must be updated alongside any command move. Moving a command in the tree alone is insufficient: it can change its result schema, creation requirements, or eligibility for remote calls.

## Command grammar

| Form | Assessment |
| --- | --- |
| `dg ticket accept 42` | Recommended. The action is a stable subcommand with its own help, arguments, flags, completion, and dotted method name. Collection operations use the same structure. |
| `dg ticket 42 accept` | Readable for one ticket, but needs identifier-aware routing outside the ordinary command tree. Collection operations such as create and list need a different grammar. |
| `dg accept 42` | Useful existing shorthand. Retain it as a compatibility entry point while documenting the grouped form. |

Support one canonical grouped grammar. Do not add both identifier-first and action-first forms.

## Proposed command map

Angle brackets denote required arguments; square brackets denote optional ones. Preserve the current identifier optionality and project-selection rules for existing operations. The new runs command requires an explicit identifier.

| Area | Canonical command | Existing entry point retained |
| --- | --- | --- |
| Inbox | `dg` | Unchanged; JSON-RPC method remains `inbox` |
| Create | `dg ticket create [title] [body]` | `dg ticket [title] [body]`, subject to the collision rules below |
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

### Creation collisions

A title is arbitrary text, so `dg ticket "runs" "42"` cannot mean both creation and history lookup. Quoting does not resolve this: the shell passes the same argument values.

Recommended rules:

1. Canonical creation becomes `dg ticket create ...` when the broader reorganisation is implemented.
2. During migration, bare `dg ticket` retains editor-based creation, and legacy creation remains available for ordinary titles.
3. A first positional word matching an installed ticket subcommand selects that subcommand. Invalid arguments produce that subcommand's error; never fall back to creating a ticket after an action fails.
4. Use `--` after the command and its flags to treat subsequent words literally. For example, `dg ticket -- "runs" "42"` creates a ticket with that title and body. The future explicit form `dg ticket create -- "runs" "42"` is unambiguous too.
5. Only installed subcommand names become reserved. Document the newly reserved names in each release. Titles and bodies containing an action word elsewhere remain ordinary text.
6. Keep bare creation during the compatibility period. A future change to make bare `dg ticket` display help requires a separate decision.

This preserves ordinary legacy usage, but cannot preserve every ambiguous invocation unchanged. The collision and literal form must be documented rather than described as fully transparent compatibility.

### Existing commands and scripts

Keep root-level entry points callable with their existing flags, defaults, output, exit behaviour, and validation. Share execution logic through command factories or operation functions; do not maintain separate implementations of the same action.

Do not reparent the same mutable Cobra command instance in two places. Build separate command instances using shared handlers and independently bound flags. Creation-only flags must not become inherited flags on every ticket command.

Initially, show compatibility commands with a short pointer to the canonical form. Avoid automatic per-call warnings that add noise to scripts or corrupt structured output. No removal date is proposed. Removing old commands or changing bare creation needs an explicit release decision.

## Structured methods and discovery

Canonical methods follow the command tree: `ticket.create`, `ticket.show`, `ticket.accept`, `ticket.runs`, `queue.pause`, and `queue.start`.

Retain existing method names (`ticket` for creation, `show`, `accept`, `pause`, and so on) as compatibility methods. Shell command aliases alone do not provide this: the current discovery walker derives names from each command's primary path.

For each old/new pair:

- Both names resolve to the same operation, argument semantics, and result schema.
- Discovery publishes both callable names during migration and identifies the preferred name in the method description.
- Interactive restrictions follow the operation. `ticket.chat` remains unavailable through JSON-RPC just as `chat` is today. Creation still requires an explicit project for remote callers. Editor and standard-input restrictions remain intact.
- Routing honours the selected method. A request with method `ticket` and arguments `["runs", "42"]` must remain creation. Positional data must not redirect an already resolved request into a child command.
- Flags and literal arguments remain distinct through argument conversion. Test action-like titles, leading dashes, spaces, and shell punctuation. Do not implement routing by assembling shell text.
- Namespace-only parents such as the future `queue` group are not advertised as executable methods unless they have a defined operation and result.

Adding grouped names is additive; existing payload schemas retain their meaning. The desktop can continue using existing methods while adopting new ones individually. Do not require a coordinated desktop/backend cutover.

## Separate run history command

### Request and response

`dg ticket runs <id>` lists every recorded run for that ticket, newest first by run identifier, using the store query delivered by ticket #378. The proposed JSON-RPC method is `ticket.runs` with exactly one positive ticket identifier in `params.args`.

Proposed response envelope:

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

Terminal output should show the same recorded history in a compact table, with an explicit empty-history message and readable missing values. Register the result schema in discovery and regenerate the command reference with `make docs`.

### Small initial addition

The runs command can land before the broader reorganisation. Add it beneath the existing ticket-creation command, retaining the parent's current behaviour. Reserve only `runs` at this stage, support the literal-title escape, and protect existing JSON-RPC creation routing. Add no other grouped commands in this ticket. That keeps the immediate feature independent of the pending review.

The original #379 proposal added history to `show`; the replacement command supersedes that approach. Its prerequisite is #378. Cancellation of #379 is being handled separately by the user.

### Desktop loading

Opening Overview requests `show`; it does not request run history. On the first visit to Runs, fetch `ticket.runs` for the current ticket and connection. Show a loading state, a retryable error, or a confirmed empty-history state as appropriate.

Cache by connection and ticket identifier. Returning to Runs can reuse the result until refresh invalidates it. If Runs is active, manual refresh reloads its history. Otherwise, refresh marks that history stale and the next visit reloads it. Future automatic refresh should request history only for the active Runs tab. Clear caches on connection changes and reject late responses for a different selection or connection. Future ticket actions invalidate affected history after success.

Dependencies remain in inbox/show because those views need them immediately. Separating runs does not move dependency retrieval behind another tab or add a request per inbox row.

## Delivery approach after review

These are proposed stages, not queued implementation tickets:

1. Deliver the independently requested runs command, schema, literal-title handling, and compatibility checks.
2. Establish canonical creation and shared command factories, with exact operation routing for remote requests.
3. Add grouped read commands while retaining their root entry points.
4. Add grouped ticket mutations in small slices, preserving validation and interactive restrictions.
5. Add the queue group and retain existing start/pause entry points.
6. Make help, completion, examples, and discovery consistently favour canonical names; migrate first-party callers incrementally.

Each later ticket should be independently reviewable, target roughly 200 production lines or fewer, and link actual prerequisites. Split dispatch foundations from command integration where needed. Do not create those tickets until this design has been reviewed.

## Verification requirements

- Old/new command parity for flags, project defaults, output, errors, and state transitions.
- Legacy creation, editor invocation, literal action titles, and invalid subcommand arguments.
- Exact remote-method routing: creation data cannot choose a different operation.
- Discovery includes the right result schema and argument bounds for every callable method; namespace parents and interactive commands remain correctly excluded.
- Explicit project requirements and editor restrictions still apply to canonical creation/editing.
- Runs covers no history, multiple restarts, equal start timestamps, missing metadata, active/ended runs, and unknown tickets.
- Desktop Runs requests are lazy, survive normal tab switching, invalidate correctly, and ignore stale responses.
- Generated reference and shell completion reflect the canonical structure without concealing compatibility forms.

## Decisions for review

| Decision | Recommended choice |
| --- | --- |
| Ticket grammar | Action before identifier |
| Creation name | `ticket create` |
| Bare `dg ticket` during migration | Keep editor/legacy creation |
| Ambiguous title handling | Subcommand takes precedence; document the literal `--` form |
| Queue controls | `queue start` and `queue pause` |
| Existing commands and remote methods | Retain; no removal deadline in this change |
| Run-history retrieval | Separate `ticket.runs`, fetched when Runs is opened |
| Broader implementation planning | Wait for review before creating tickets |
