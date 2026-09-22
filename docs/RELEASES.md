# Alcubi Delegator releases

This document is the contract for release artifacts and the procedure that the
publishing workflow and installers use. The command is always named `dg`;
Alcubi Delegator is the human-facing product name.

## Versions and tags

A release version is semantic versioning without a leading `v`, such as
`1.4.0` or `1.4.0-rc.1`. Its Git tag is that version with one leading `v`, such
as `v1.4.0`. A release build reports the tag form: `dg version` prints
`dg v1.4.0`. Tags identify clean commits and are never moved or reused.

Snapshot versions have the form `0.0.0-snapshot-<commit>` and validation builds
use `0.0.0-validate`. They are local artifacts, not published versions.

## Artifact contract

Releases support `amd64` and `arm64` on Linux, macOS, and Windows. The operating
system identifiers in filenames are `linux`, `darwin`, and `windows`; macOS uses
Go's stable `darwin` identifier. Linux and macOS archives are `tar.gz` files and
Windows archives are ZIP files:

```text
alcubi-delegator_<version>_<os>_<arch>.tar.gz
alcubi-delegator_<version>_windows_<arch>.zip
```

Each binary archive contains exactly `dg` (or `dg.exe` on Windows), `LICENSE`,
and `TRADEMARKS.md` at its root. It contains no man pages, shell completions, or
documentation-site copy. The separate source distribution is named
`alcubi-delegator_<version>_source.tar.gz`; it contains the tracked source tree,
including `LICENSE` and `TRADEMARKS.md`.

`alcubi-delegator_<version>_checksums.txt` covers all six binary archives and
the source archive. Each line is the lowercase hexadecimal SHA-256 digest,
whitespace, and the artifact basename. Lines are sorted by basename.

For a tag `<tag>` and artifact `<file>`, a client derives the stable URL without
scraping a page:

```text
https://github.com/alcubie/delegator/releases/download/<tag>/<file>
```

For example, the Linux arm64 archive for `v1.4.0` is:

```text
https://github.com/alcubie/delegator/releases/download/v1.4.0/alcubi-delegator_1.4.0_linux_arm64.tar.gz
```

Command documentation is the published [Alcubi Delegator CLI reference](https://alcubi-delegator.readthedocs.io/en/latest/).
It is not copied into binary archives.

## Reproducible dry runs

GoReleaser is pinned in the Makefile and invoked with `go run`, so no separately
installed release tool is selected accidentally. These commands never publish
and need no release credential:

```sh
make release-check
make release-snapshot
make release-validate
```

`release-check` validates the configuration. `release-snapshot` creates the
platform and source archives under `dist`. `release-validate` makes a clean
validation build and checks artifact names, archive contents and timestamps,
checksums, and the version embedded in every binary. It also runs the native
binary and checks its reported version. Go module downloads may use the normal
shared Go cache; `dist` is ticket- or checkout-local.

The build disables cgo, trims source paths, omits the Go build ID, and gives
binaries and archive entries the source commit time. Repeating a build from the
same clean commit with the same Go and pinned GoReleaser versions therefore
does not introduce wall-clock timestamps. Keep the Go toolchain version fixed
in the publishing workflow as well.

## Publishing handoff

The publishing workflow must first run `make check`, `make integration`, and
`make release-validate` on the intended clean commit. It then creates the
`v<version>` tag and runs the pinned tool on that exact tag with
`go run github.com/goreleaser/goreleaser/v2@v2.17.1 release --clean`. The
version in this command and `GORELEASER_VERSION` must change together.
Credentials belong only in the publishing environment; they do not belong in
this repository, GoReleaser configuration, command output, or archives. Before
making the release public, compare its artifact names and checksum file with
this contract and confirm that `dg version` reports the tag.
