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

- **Run the real binary.** `make install` puts `dg` on the PATH. Two faults this
  session passed each test and appeared at the first real run: `dg show` wrapped
  prose that already held the line breaks of the person, and the text of a field
  went past the rule below the title. A test holds the data that its writer
  thought of, and a real ticket holds the data that a person wrote.
