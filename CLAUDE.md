# delegator

The code of the product. The ground rules of the prototype are in the CLAUDE.md
above this directory, and this file holds what belongs to this repository only.

## Layout

Read this before you go looking. It is here so that a run does not have to
derive the shape of the repository by reading it.

- `cmd/dg` finds the directories and executes the cobra tree. `cmd/dg-fake-agent`
  is the Agent Client Protocol agent the tests drive instead of a real one.
- `internal/cli` is the command layer: the cobra tree, the work behind each
  command, and the text a command writes. One file per command, named for it,
  so `dg ticket move` is `move.go`.
- `internal/store` keeps every field in one SQLite database, and owns the
  migrations. The prose of a ticket is not in it; that is a file under
  `tickets/` in the data directory.
- `internal/run` starts a run and supervises it. The prompt the agent gets is
  `prompt` in `supervisor.go`; `internal/handler` starts its ACP session.
- `internal/project` is the only package that starts git.
- `internal/inbox` decides which ticket is in which group and in what order,
  and writes no text.
- `internal/config` describes the instance settings stored in SQLite.
- `internal/testfix` holds the fixtures that more than one package's tests need.

`make check` runs the formatter, vet, staticcheck and the coverage floor, and
the pre-commit hook runs it. `make test` alone is faster while you work.
`make integration` drives a real agent and costs money, so it is not in
`make check`.

Keep this section at the level of a directory. A section that named files
would be wrong the first time a file was added, and an agent that trusts a
wrong map spends more than one with no map at all. When a run goes looking in
the wrong place, that is the signal to correct this.

## Tests

- **Mutation testing needs `go test -a`.** After a test goes green, break the
  thing the test names and confirm that it goes red. Run that with
  `go test -a -count=1`. The flag `-count=1` bypasses the cache of results only,
  and a mutation that changes one file of the code can leave the binary of the
  test unbuilt, so the run says `ok` against code that is not there any more. A
  mutation that lives is not a mutation that lives until `-a` says so. The
  symptom: `staticcheck` reports the change (a function that nothing calls, for
  example) and `go test` passes.

- **A mutation can leave files in the repository.** `go test` runs each test
  binary with the package directory as its working directory, so a mutation that
  corrupts a path writes there and not into a temporary directory. After a
  mutation run, read `git status` before staging, and stage the paths you
  changed rather than everything.

- **Run `make integration` after changing the agent path.** The tests behind
  the build tag `integration` drive a real agent; they cost money, so
  `make check` only compiles them and `make release` runs them. `go test` will
  not list them and nothing runs them for you. Run them when you change the
  prompt in `internal/run/supervisor.go` or how `run.Start` captures output,
  and say in the commit message that they passed.

- **A launch that defaults to `os.Executable` is the test binary under test.**
  `launch` in `internal/cli` starts dg's own executable with `run <id>`. Under
  `go test` that executable is the test binary, so every `dg ticket create` in the
  tests started a detached copy of the tests, which started more, and the CPU
  sat at 100% until they burned out. `TestMain` sets `launch` to a no-op for
  the package; a new package-level launcher needs the same.

- **Run the real binary.** `make install` puts `dg` on the PATH. Two faults this
  session passed each test and appeared at the first real run: `dg ticket show` wrapped
  prose that already held the line breaks of the person, and the text of a field
  went past the rule below the title. A test holds the data that its writer
  thought of, and a real ticket holds the data that a person wrote.

## Comments

- **A comment says what the code does now.** Do not write a comment that
  refers to code that nothing uses yet, or to a ticket that will use it later:
  `No command reads the values yet; ticket 22 is the first`. The sentence is
  true on the day it is written and wrong on the day that ticket lands, and
  nobody comes back to delete it. What a later ticket will do belongs in that
  ticket and in the commit message, which is a record of a moment and does not
  go stale.
- Don't write inline comments unless I ask about code to be explained. Only then add an inline comment.
- Keep functional comments brief.
- Never modify functional comments in a commit where the code of the function didn't change.
