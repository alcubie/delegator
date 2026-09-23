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

## Prepare and tag

Run `make release-prepare` before tagging. This runs `make docs`, shows status
and the generated diff under `docs/public/reference`, and fails if that directory
has uncommitted changes (including new files). Review the diff, stage and commit
it yourself, and rerun preparation. Nothing is staged or committed automatically.
Run `make check` on the complete reviewed tree. The tag workflow never runs
`make docs`: `make docs-check`, through `make check`, compares the committed
reference with `cli.Root` and builds the MkDocs site strictly in temporary paths.
Either failure stops the release before any upload.

Before the first production tag, maintainers must complete the licensing and
source-availability decisions. Configure the GitHub `release` environment with
required reviewers and tag restrictions, and protect version tags against
unreviewed creation, modification, and deletion. Review the
[FSL-1.1-ALv2 license](../LICENSE) and [trademark policy](../TRADEMARKS.md).
This workflow does not establish trademark registration or clearance.

After reviewing the source and release notes, create an annotated tag:

```sh
test -z "$(git status --porcelain)"
git tag -a v1.4.0 -m 'Alcubi Delegator v1.4.0'
git push origin v1.4.0
```

Use `v1.4.0-rc.1` for a public prerelease. Every tag starting with `v` triggers
the workflow; invalid semantic versions fail before building. Production and
prerelease tags use the same checks and six-target build. Go 1.26.6 and the
Makefile's GoReleaser pin define the release toolchain. Artifact names remain
those specified above; the executable remains `dg`.

## Dry run and publication

In Actions, select **Release → Run workflow** on the reviewed branch. This
manual path runs `make check` and `make release-validate` with a unique
`0.0.0-rc.<run-number>` version. It retains the seven archives and one canonical
checksum file as the `release-assets` workflow artifact for seven days. It
cannot enter the publishing job, create a tag, or create a repository release.
The local equivalent is `make check` followed by
`make release-validate VALIDATION_VERSION=0.0.0-rc.1`.

Normal checks include formatting, vet, lint, coverage, archive policy, the
committed CLI reference, and the strict site build. The credential-free runner
compiles the optional live-agent integration tests through `make check`, but
does not run `make integration`: these tests require installed, authenticated
third-party agents and can incur charges. Maintainers can additionally run
`make release` in an authenticated development environment before tagging.

For tags, `make release-build` uses the pinned GoReleaser with `--skip=publish`,
then validates every archive and embedded version. A clean-tree check follows.
The separate publishing job receives only validated artifacts, has
`contents: write`, and runs behind the `release` environment. Build jobs have
only `contents: read`; checkout does not persist credentials. There is no
pull-request trigger and no agent or personal-access credential. The temporary
GitHub token is supplied only to the publication step, without shell tracing.

Publication first creates a **draft**, uploads all eight files, downloads them
again, compares the canonical checksum file, and verifies all seven checksums.
Only then does it make the release public. Prerelease tags are marked as such;
the workflow does not change the repository's “Latest” selection automatically.
A required test, build, validation, or upload failure cannot produce a new
public release with an incomplete set of assets.

Release titles use **Alcubi Delegator**. Notes combine GitHub's generated change
list with verification guidance and links to the tagged FSL license and
`TRADEMARKS.md`. Review the included changes before pushing the tag; maintainers
can edit prose afterward without changing the tagged source or artifacts.
See the [GoReleaser publication controls](https://goreleaser.com/getting-started/quick-start/)
and [GitHub release commands](https://cli.github.com/manual/gh_release_create).

## Retry and recovery

Inspect the failed Actions run first. Failures before publication create no
release. Retry a transient failure on the same immutable tag using **Re-run all
jobs**. If an upload or verification failed, a private draft may remain: inspect
it and delete **only the draft release**, retaining its tag, before rerunning.
The workflow deliberately refuses to overwrite any existing release or draft.
If publication succeeded but the job response was lost, inspect the public
release and verify its assets; do not delete or replace it just to rerun CI.

Source, documentation, or build-configuration fixes require a new commit and a
new version tag. Never move a tag, reuse a published version, or replace public
assets. Concurrent runs for one ref are serialized without cancellation.

## Verify on a clean machine

Download the archive for your OS/architecture and the checksum file from the
same tagged repository release. For example, on Linux amd64:

```sh
version=1.4.0
base="https://github.com/alcubie/delegator/releases/download/v$version"
archive="alcubi-delegator_${version}_linux_amd64.tar.gz"
checksums="alcubi-delegator_${version}_checksums.txt"
curl --fail --location --remote-name "$base/$archive"
curl --fail --location --remote-name "$base/$checksums"
grep "  $archive$" "$checksums" > selected-checksum.txt
test "$(wc -l < selected-checksum.txt)" -eq 1
sha256sum --check selected-checksum.txt
tar -xzf "$archive"
./dg version
```

Expect `dg v1.4.0`. On macOS use the Darwin archive and
`shasum -a 256 -c selected-checksum.txt`; on Windows use the matching ZIP,
compare `Get-FileHash -Algorithm SHA256` with its entry in the checksum file,
then `Expand-Archive` and run `.\dg.exe version`. Checksums detect corruption;
obtain both files from the trusted repository release. Read the bundled license
and trademark policy before redistribution. The source archive and its checksum
are available alongside the binaries for inspecting the exact tagged source.
