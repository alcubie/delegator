# Making a release

Pushing a version tag publishes a release through GitHub Actions.
`make release` only runs local checks and builds artifacts; it does not publish.

## One-time setup

The project uses [FSL-1.1-ALv2](../LICENSE) and publishes source alongside
binaries. See the [trademark policy](../TRADEMARKS.md) for naming and branding.

Before the first public release:

1. Review the repository and Git history, then make the repository public.
2. In **Settings → Environments**, create `release`. Add yourself as a required
   reviewer, leave **Prevent self-review** off while you are the only maintainer,
   and disable administrator bypass. Under **Deployment branches and tags**,
   select **Selected branches and tags** and allow only **Tag → `v*`**.
3. In **Settings → Rules → Rulesets**, create these two **active tag rulesets**:

   | Name | Target | Enabled rules | Bypass |
   | --- | --- | --- | --- |
   | `release-tag-creation` | `v*` | Restrict creations | Repository admin, Always allow |
   | `release-tag-immutability` | `v*` | Restrict updates; Restrict deletions | None |

   Leave all other rules off, including **Block force pushes**. The admin bypass
   assumes you are the only administrator; otherwise use a team containing only
   authorized release maintainers.

The workflow already uses the `release` environment and GitHub's temporary
publishing token. No additional release secret is needed. See GitHub's
[environment instructions](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)
and [ruleset instructions](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/creating-rulesets-for-a-repository)
for the settings above.

## Release checklist

1. Run `make release-prepare`. If it changes the CLI reference, review and commit
   those changes, then rerun it.
2. Run `make check`. For the full local gate, use `make release` instead: it also
   runs [live-agent integration tests](INTEGRATION_TESTS.md) and validates release
   artifacts. Those tests require authenticated agents and can incur charges;
   GitHub Actions does not run them.
3. Review the changes being released and commit all intended changes. Choose a
   new semantic version: `v1.4.0` for a stable release or `v1.4.0-rc.1` for a
   prerelease. From the clean, checked commit, create and push an annotated tag:

   ```sh
   make release-tag v1.4.0  # Replace with the version you are releasing.
   ```

4. Open **Actions → Release**. After checks and builds pass, review and approve
   the `release` environment deployment. The workflow uploads a draft, downloads
   its assets to verify checksums, then publishes it with generated release notes.
5. Download the archive for your machine and
   `alcubi-delegator_1.4.0_checksums.txt` from the release into the same directory.
   In a terminal, change to that directory. Replace `1.4.0` and the archive name
   below with your release version and downloaded filename (`amd64` for Intel/AMD
   or `arm64` for ARM, including Apple silicon).

   On macOS, verify the archive against its entry in the checksum file:

   ```sh
   archive=alcubi-delegator_1.4.0_darwin_arm64.tar.gz
   grep -F "  $archive" alcubi-delegator_1.4.0_checksums.txt | shasum -a 256 -c
   ```

   On Linux:

   ```sh
   archive=alcubi-delegator_1.4.0_linux_amd64.tar.gz
   grep -F "  $archive" alcubi-delegator_1.4.0_checksums.txt | sha256sum -c
   ```

   Both commands must print the archive filename followed by `OK`.

   On Windows, use PowerShell:

   ```powershell
   $archive = 'alcubi-delegator_1.4.0_windows_amd64.zip'
   $entry = Get-Content 'alcubi-delegator_1.4.0_checksums.txt' |
     Where-Object { ($_ -split '\s+')[1] -eq $archive }
   if (@($entry).Count -ne 1) { throw 'Expected exactly one checksum entry' }
   $expected = ($entry -split '\s+')[0]
   $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash
   if ($actual -ne $expected) { throw 'Checksum mismatch' }
   Write-Host "$archive`: OK"
   ```

   If verification fails or no matching entry is found, stop and download the
   files again. After a successful check, extract the archive and run
   `./dg version` (`.\dg.exe version` on Windows). For the example above, expect
   `dg v1.4.0`.
6. For a stable release intended for default installation, edit the GitHub release
   and mark it **Latest**. The workflow does not do this automatically; the
   installer defaults to the latest stable release.

Never move or reuse a version tag, or replace published assets.

## Dry run

In **Actions → Release → Run workflow**, select the branch to validate. This
runs checks and builds all archives without publishing or creating a tag.
Download `release-assets` from the run within seven days.

Locally, run:

```sh
make check && make release-validate
```

Artifacts go in `dist/`. For narrower checks, `make release-check` validates only
GoReleaser configuration, and `make release-snapshot` builds snapshot artifacts.
None of these commands publish.

## If a release fails

- Inspect the failed Actions step. For a transient failure, use **Re-run all jobs**.
- If a draft remains, inspect and delete only that draft before rerunning. Keep
  its tag; the workflow refuses to overwrite an existing release or draft.
- If the release is already published, verify its assets instead of deleting it.
- If source, docs, or build configuration need fixing, commit the fix and use a
  new version tag.

## Release files

Each release has eight files: six binary archives, one source archive, and one
SHA-256 checksum file. `<version>` omits the tag's leading `v`.

| Contents | Filename |
| --- | --- |
| Linux/macOS binary | `alcubi-delegator_<version>_<os>_<arch>.tar.gz` |
| Windows binary | `alcubi-delegator_<version>_windows_<arch>.zip` |
| Tagged source tree | `alcubi-delegator_<version>_source.tar.gz` |
| Checksums for all seven archives | `alcubi-delegator_<version>_checksums.txt` |

`<os>` is `linux` or `darwin`; `<arch>` is `amd64` or `arm64`. Binary archives
contain exactly `dg` (Windows: `dg.exe`), `LICENSE`, and `TRADEMARKS.md` at their
root. The source archive includes the tracked source and policy files. Checksum
lines contain the lowercase digest, whitespace, and filename, sorted by filename.

Download URLs follow
`https://github.com/alcubie/delegator/releases/download/<tag>/<filename>`.
Build and validation details live in the [Makefile](../Makefile),
[GoReleaser config](../.goreleaser.yaml), and
[release workflow](../.github/workflows/release.yml).
