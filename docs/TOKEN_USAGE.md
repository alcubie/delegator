# Delegator: the tokens and the model of a run

**Condition:** draft, for your examination
**Date:** 2026-09-08
**Companion to:** [`TECHNICAL_DESIGN.md`](TECHNICAL_DESIGN.md)
**Language:** ASD-STE100 Simplified Technical English, Issue 9.

Ticket 46 asks delegator to keep the tokens of a run. Ticket 48 asks it to keep the
model. The work on ticket 46 is complete on a branch, and its examination found data
that changes both tickets. This document gives that data, the decisions that come from
it, and the sequence of the work.

## 0. Technical names

Section 0 of the technical document declares the technical names, and this document uses
them. It adds these:

| Technical name | What it is in this document |
|---|---|
| token | The unit that an agent counts for the work of a model. |
| model | One model of one provider, such as `claude-opus-5`. |
| provider | The company that makes a model, such as `anthropic`. |
| canonical name | The name of a model with no date and no suffix. `modelUsage` gives it. |
| primary model | The model that does the work of a run. A run also uses a smaller model. |
| prompt | The full text that a model reads for one message. |
| prompt cache | The store of the agent. It keeps a prompt and gives it again at a lower price. |
| input tokens | The part of the prompt that the prompt cache did not give and did not keep. |
| cache write tokens | The part of the prompt that one message put into the prompt cache. |
| cache read tokens | The part of the prompt that the prompt cache gave. |
| output tokens | Each token that a model made. |
| thinking tokens | The part of the output tokens that the person does not see. |
| usage | What one model consumed in one run. |
| price | The cost of 1 million tokens. |
| cost | The value of a usage, in US dollars. |
| stream | The lines of JSON that an agent writes while a run continues. |
| result event | The last line of the stream. It gives the totals of the run. |
| message start event | A line of the stream. It gives the 3 input counts of one message. |
| rate limit event | A line of the stream. It gives the use of the limits of the person. |
| table | One table of the database. |
| column | One column of a table. |
| row | One row of a table. |
| index | An index of SQLite. |
| view | A view of SQLite. It gives the columns of one or more tables with one name. |

## 1. What the agent reports

The agent reports the usage of a message in 4 counts. No token is in more than one
count, thus the prompt of a message is the sum of the 3 input counts. The 3 input counts
have 3 prices. The price of a cache read token is about one tenth of the price of an
input token. The price of a cache write token is about 1.25 times the price of an input
token. A total of the 3 gives the number of tokens that a run read, but it does not give
the cost.

The 4 counts of one run are far apart. The run of ticket 42 is an example:

| Count | Tokens |
|---|---|
| input tokens | 172 |
| cache write tokens | 127,540 |
| cache read tokens | 7,221,701 |
| output tokens | 49,997 |

Each of the 13 runs in the log directory used 2 models. Each run used its primary model
for the work, and `claude-haiku-4-5` for about 1,000 input tokens and 14 output tokens.
The primary model was not the same for each run: 8 runs used `claude-opus-5` and 5 runs
used `claude-fable-5-1`. The price of `claude-fable-5-1` is 2 times the price of
`claude-opus-5`. Thus the model is necessary before delegator can give the cost of a
run.

The result event gives `modelUsage`, which has one part for each model. Each part gives
12 fields: `inputTokens`, `cacheCreationInputTokens`, `cacheReadInputTokens`,
`outputTokens`, `thinkingTokens`, `webSearchRequests`, `costUSD`, `costBasis`,
`contextWindow`, `maxOutputTokens`, `canonicalModel` and `provider`. The 13 runs gave 26
parts, and all 26 have the same 12 fields. Thus one shape holds each model of claude.

The field `provider` of `modelUsage` is `firstParty` in each of the 26 parts. That field
names the route to the model and not the company that makes it. Thus the adapter gives
the name of the company, and the stream does not.

The first line of the stream names the primary model. That name is also a name in
`modelUsage`, in each of the 13 runs. The stream shows the messages of the primary model
only. Delegator can thus count those messages while the run continues. It gets the other
model at the end of the run, from `modelUsage`.

The stream also gives a rate limit event. That event gives the use of the limit of 5
hours and the limit of 7 days, as a value between 0 and 1. For a question about a limit,
that event is a better source than a count of tokens.

## 2. Other providers do not use the same counts

These counts are not a standard. The names are different, and the same name does not
always show the same thing:

- OpenAI gives one count for the prompt. The part that the cache gave is inside that
  count. There is no count for a cache write.
- Gemini gives `promptTokenCount` for the prompt. The count `cachedContentTokenCount` is
  inside that count.

Each adapter therefore keeps what its agent gives, in a table of its own. The readers do
not see the difference, because they use the view of section 4.3.

## 3. Decisions

1. Keep one row for each model of each run. A run uses more than 1 model, each model has
   its own counts, and each model has its own price.
2. One row of a run is the primary row. A unique index that contains the primary rows
   only keeps 1 primary row for each run.
3. Keep the models in a table of their own, with the provider and the name. A usage row
   points to a model, and no usage row holds the name of a model as text.
4. Make one table of usage for each adapter, with the fields that its agent gives.
   claude gives the same 12 fields for each of its models, thus one table for claude
   holds each of them and loses nothing.
5. Give the readers a view with the columns that each adapter has. The writers use the
   table of their adapter. A new adapter adds a table and adds its rows to the view, and
   no reader changes.
6. Keep one column for each count. Do not keep a total, because a total does not give
   the counts again, and the counts have different prices.
7. Keep the cost that the agent gives. Delegator keeps no list of prices, and no code of
   delegator multiplies a count by a price.
8. The table `runs` gets no column for the model and no column for a token. The
   provider is a column of the table `models`, and not a table of its own, because
   delegator keeps no other data about a provider.
9. Start claude with `--include-partial-messages`. Without it, the stream gives the
   output count of a message as the count at the start of that message. That count was
   841 for a run that made 49,997 output tokens.

## 4. The schema

### 4.1 The table of models

```sql
CREATE TABLE models (
  id        INTEGER PRIMARY KEY,
  provider  TEXT NOT NULL,
  name      TEXT NOT NULL,
  canonical TEXT,
  UNIQUE (provider, name)
);
```

The column `name` holds the name that the agent gives, with no change. The 13 runs gave
3 names, and each of the 3 has a different shape:

| Name | Shape |
|---|---|
| `claude-fable-5-1` | the name only |
| `claude-haiku-4-5-20251001` | the name and a date |
| `claude-opus-5[1m]` | the name and the selection of the 1 million context |

Delegator does not remove the date or the suffix, because a rule that removes them is a
rule about the names of one provider. The column `canonical` holds the name that
`canonicalModel` gives in `modelUsage`, which is `claude-haiku-4-5` for the second row
of the table above. It is NULL until a result event gives it.

The adapter gives the provider, because section 1 shows that the stream does not. For
claude the provider is `anthropic`.

### 4.2 The table of usage for claude

```sql
CREATE TABLE claude_usage (
  run_id              INTEGER NOT NULL REFERENCES runs(id),
  model_id            INTEGER NOT NULL REFERENCES models(id),
  is_primary          INTEGER NOT NULL DEFAULT 0 CHECK (is_primary IN (0, 1)),
  input_tokens        INTEGER NOT NULL DEFAULT 0,
  cache_write_tokens  INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens   INTEGER NOT NULL DEFAULT 0,
  output_tokens       INTEGER NOT NULL DEFAULT 0,
  thinking_tokens     INTEGER NOT NULL DEFAULT 0,
  web_search_requests INTEGER NOT NULL DEFAULT 0,
  cost_usd            REAL,
  PRIMARY KEY (run_id, model_id)
);

CREATE UNIQUE INDEX claude_usage_primary ON claude_usage(run_id) WHERE is_primary = 1;
```

Each count is NOT NULL with a default of 0, because a model that starts consumed
nothing, and 0 is that quantity. The column `cost_usd` is NULL until the result event
gives it. A cost of 0 dollars and a cost that delegator does not know are not the same.
The type of `cost_usd` is REAL because the agent gives a value with a decimal point.
Delegator only adds those values, for a total that a person reads.

The index contains the primary rows only. SQLite makes each NULL different from each
other NULL in an index, and a partial index of this shape does not have that problem: it
holds 1 row for each run, and a second primary row for the same run is an error.

This table does not keep 4 of the 12 fields. `contextWindow` and `maxOutputTokens` are
properties of the model, and the table `models` is where they go if a ticket needs them.
`costBasis` and `provider` are properties of the route to the model, which delegator
does not use.

### 4.3 The view that the readers use

```sql
CREATE VIEW run_usage AS
SELECT u.run_id, m.provider, m.name AS model, u.is_primary,
       u.input_tokens, u.cache_write_tokens, u.cache_read_tokens,
       u.output_tokens, u.cost_usd
FROM claude_usage u
JOIN models m ON m.id = u.model_id;
```

The view gives the provider and the name with the counts, thus a reader gets them in one
query. It has the columns that each adapter can give. An adapter that gives more keeps
the extra columns in its own table, as `claude_usage` keeps the thinking tokens. A second
adapter adds `UNION ALL` and the same columns from its own table. The inbox and `dg
show` do not change.

### 4.4 When delegator writes each row

1. The first line of the stream gives the name of the primary model. The supervisor gets
   the id of that model:

   ```sql
   INSERT INTO models (provider, name) VALUES (?, ?) ON CONFLICT (provider, name) DO NOTHING;
   SELECT id FROM models WHERE provider = ? AND name = ?;
   ```

2. The supervisor writes the usage row of the primary model, with `is_primary` of 1 and
   each count at 0.
3. While the run continues, the message start events and the message delta events give
   the counts of the primary model. The supervisor writes them to that row.
4. The result event gives `modelUsage`. The supervisor gets the id of each model as step
   1 does, and it writes the canonical name of each model to `models`. Then it writes
   one usage row for each model, with the thinking tokens, the web search requests and
   the cost.
5. A run that stops before its result event keeps the row of the primary model. That row
   holds the counts that the run reached, and no cost.

### 4.5 The counts in the code

The adapter gives the usage of each model to the supervisor:

```go
// Usage is what one model consumed in one run.
type Usage struct {
	Provider   string  // the company, which the adapter gives and the agent does not
	Model      string  // the name that the agent gives
	Canonical  string  // the name with no date and no suffix, empty until the result
	Primary    bool    // true for the model that does the work
	Input      int64
	CacheWrite int64
	CacheRead  int64
	Output     int64
	Thinking   int64
	WebSearch  int64
	CostUSD    float64 // 0 until the result event gives it
}
```

There is 1 adapter today, thus there is 1 table of usage and 1 type. The ticket that adds
the second adapter decides if this type stays as it is, or if each adapter gets a type.
The view of section 4.3 makes that ticket small, because it keeps the readers away from
the difference.

## 5. The sequence of the work

| Sequence | Ticket | What it does |
|---|---|---|
| 1 | 48 | Make `models`, `claude_usage` and the view. Keep the primary model of a run. |
| 2 | 56 | Keep the counts of each model, while the run continues and at its end. |
| 3 | 57 | Show the tokens of a run on the RUNNING row. |

Ticket 48 is first because the model is the name of a row, and the counts of ticket 56
go in that row. Ticket 56 does the work of ticket 46 again, in the shape that section 4
gives. We do not merge the branch of ticket 46. Ticket 57 shows the tokens to the
person, and ticket 46 named it as the work that comes after.
