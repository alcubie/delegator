# Controlling Alcubi Delegator token costs

An agent run can cost more tokens than expected because the model reads its
growing conversation again on later turns. Early repository exploration stays
in that context for the rest of the run. Long runs can therefore spend much of
their input on text that the agent read before it changed a file.

## Give the agent a repository map

Put a short repository map in the instruction file that the selected agent
reads when it starts. For example, Claude reads `CLAUDE.md`. Include:

- one line for each directory and the work that belongs there;
- the commands to build, test, format, and lint;
- links to project conventions; and
- the few entry points that a new contributor normally needs.

Keep the map at directory level. A list of every file becomes incorrect after
ordinary repository changes. Update the map when an agent searches in the
wrong place.

Also tell the agent which areas a ticket affects when you know them. Prefer a
ticket that names one change over a broad request to discover and solve an
unspecified problem. These choices reduce exploration, but they do not set a
hard token limit.

## Evidence from this project

Seven Delegator runs measured on September 4, 2026, put between 17,000 and
91,000 tokens into context before the first code change. Carrying that context
through the remainder of each run accounted for 21 to 48 percent of the run.
The runs opened 75 different files, and only 13 files appeared in four or more
runs. The repeated cost was not one shared set of files. Each run reconstructed
the repository layout for itself.

This is project evidence, not a price or usage guarantee. Agents and providers
report tokens differently, model prices change, and some agents omit usage.
Delegator stores only the aggregate counts that an ACP agent reports. It does
not calculate billing or enforce a spending limit.

See [the token accounting design](TOKEN_USAGE.md) for the measurements, data
model, and differences between provider reports. See the [privacy
notice](../PRIVACY.md) for the usage data stored locally and for third-party
agent processing.
