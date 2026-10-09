# Making a release

Pushing a version tag publishes a release through GitHub Actions without marking
it **Latest**. Run `make release-latest` after publication to verify the assets and
select the release for default installation. A stable version
tag has no prerelease suffix, for example `v1.4.0` rather than `v1.4.0-rc.1`.
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
   those changes, then rerun it. Confirm the privacy notice describes the shipped
   telemetry behavior before distributing a telemetry-capable binary; generated
   release notes link the notice and state that telemetry requires an explicit
   choice.
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

4. Open **Actions → Release**. After Linux/macOS checks, builds, and Windows
   installer checks pass, review and approve the `release` environment deployment. The workflow uploads a draft, downloads
   its assets to verify the seven archive checksums and compare both installers
   byte for byte, then publishes it with generated release notes and leaves
   **Latest** unchanged. Windows checks
   run the retained `install.ps1` under Windows PowerShell 5.1 and PowerShell 7;
   a failed check blocks publication. The publication job also checks both scripts
   against the source hashes retained by the build before creating the draft.
5. Once publication succeeds, verify and promote the stable release with one
   command (replace `v1.4.0` with the release tag):

   ```sh
   make release-latest v1.4.0
   ```

   This requires Python 3 and an authenticated GitHub CLI with release write access. It rejects
   drafts and prereleases, downloads all ten assets, verifies all seven archive
   checksums, and requires both nonempty installers before changing **Latest**.
   A failed download, missing asset, or checksum mismatch stops promotion.
   Expect `Latest is now v1.4.0` on success. Temporary downloads are removed.
   The command can also select an older stable release for rollback; it does not
   enforce version order. Use releases published through the
   normal workflow: this command does not repeat the installer source-byte
   comparisons or Windows tests. Prerelease tags cannot be promoted; publish a
   new stable version tag when ready.
   Changing **Latest** affects future default installations, not existing ones.
6. (Optional) Publish a new version of the website docs for minor version changes.

Never move or reuse a version tag, or replace published assets.

To remove the approval step for a single maintainer, remove **Required reviewers**
under **Settings → Environments → release**. Keep the `v*` deployment restriction
and tag rulesets above. This makes an authorized tag push sufficient to publish
once automated checks pass. Verification and promotion to **Latest** remain
a single manual step with `make release-latest`.

## Public installer URLs

The website's `public/_redirects` maps
`https://alcubi.ai/delegator/install.sh` to
`https://github.com/alcubie/delegator/releases/latest/download/install.sh`.
The release workflow attaches the tested `install.sh` and `install.ps1` from
the tagged source and compares both downloaded copies before publishing. No
website deployment is needed for each subsequent release. `make release-latest`
verifies the assets and selects the stable release served by this URL.

The Windows public URL is `https://alcubi.ai/delegator/install.ps1`,
redirecting to
`https://github.com/alcubie/delegator/releases/latest/download/install.ps1`.
The README uses this live public URL. Before marking a stable release **Latest**,
verify it contains both installer assets published through the normal validated
release process. After changing **Latest**, verify that each public URL resolves
to the corresponding release asset. The Windows URL cannot serve the installer
if the selected latest release lacks `install.ps1`.

If an older release is missing the shell installer asset and its source tag
contains `install.sh`, add it from that exact local tag (replace the example tag
as needed):

```sh
make release-upload-installer v0.0.3
```

This requires GitHub release write access. It uploads only `install.sh`, refuses
to overwrite an existing asset, and verifies the uploaded copy. Existing archives
and checksums remain unchanged. This helper does not upload `install.ps1` and
cannot supply a script absent from the tag. Do not copy a newer installer into an
older release or overwrite immutable assets. If GitHub disallows adding assets
to the release, publish a new version using the corrected workflow and mark it
**Latest**.

Verify the public redirect and installer help without installing a binary:

```sh
curl -fsSL https://alcubi.ai/delegator/install.sh | sh -s -- --help
```

See [Windows installation](WINDOWS_INSTALLATION.md) for installer options, help,
upgrade behavior, and validation commands.

## Dry run

In **Actions → Release → Run workflow**, select the branch to validate. This
runs Linux/macOS checks, builds all archives, and tests the retained Windows
installer with both PowerShell versions without publishing or creating a tag.
Confirm both Windows matrix jobs pass. Download `release-assets` from the run
within seven days; inspect the ten files listed below, including nonempty
`install.sh` and `install.ps1` matching the selected commit byte for byte. The
checksum file must still have seven archive entries. A dry run does not exercise
GitHub draft upload/download; `python3 scripts/test-release.py` exercises that
publication script with controlled assets and failures.

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

Each new release has ten files: six binary archives, one source archive, one
SHA-256 checksum file, and two installers. `<version>` omits the tag's leading `v`.

| Contents | Filename |
| --- | --- |
| Linux/macOS binary | `delegator_<version>_<os>_<arch>.tar.gz` |
| Windows binary | `delegator_<version>_windows_<arch>.zip` |
| Tagged source tree | `delegator_<version>_source.tar.gz` |
| Checksums for all seven archives | `delegator_<version>_checksums.txt` |
| Shell installer from the tagged source | `install.sh` |
| PowerShell installer from the tagged source | `install.ps1` |

`<os>` is `linux` or `darwin`; `<arch>` is `amd64` or `arm64`. Binary archives
contain exactly `dg` (Windows: `dg.exe`), `LICENSE`, and `TRADEMARKS.md` at their
root. The source archive includes the tracked source and policy files. Checksum
lines contain the lowercase digest, whitespace, and filename, sorted by filename.
The checksum file covers the archives; the workflow verifies both installers
separately by comparing their bytes before and after upload.

Download URLs follow
`https://github.com/alcubie/delegator/releases/download/<tag>/<filename>`.
Build and validation details live in the [Makefile](../Makefile),
[GoReleaser config](../.goreleaser.yaml), and
[release workflow](../.github/workflows/release.yml).
