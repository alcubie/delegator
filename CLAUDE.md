# delegator

The code of the product. The ground rules of the prototype are in the CLAUDE.md
above this directory, and this file holds what belongs to this repository only.

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
  corrupts a path writes there and not into a temporary directory. Two files
  landed in `internal/fakeagent` this way and reached a commit through
  `git add -A`. After a mutation run, read `git status` before staging, and
  stage the paths you changed rather than everything.

- **Run `make integration` after changing the agent path.** The tests behind
  the build tag `integration` drive a real agent; they cost money, so
  `make check` only compiles them and `make release` runs them. `go test` will
  not list them and nothing runs them for you. Run them when you change an
  adapter in `internal/adapters`, the prompt in `internal/run/supervisor.go`,
  or how `run.Start` captures output, and say in the commit message that they
  passed.

- **Run the real binary.** `make install` puts `dg` on the PATH. Two faults this
  session passed each test and appeared at the first real run: `dg show` wrapped
  prose that already held the line breaks of the person, and the text of a field
  went past the rule below the title. A test holds the data that its writer
  thought of, and a real ticket holds the data that a person wrote.
