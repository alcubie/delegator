# Windows installation

Run in your current Windows PowerShell 5.1 or PowerShell 7 session:

```powershell
iwr -useb https://alcubi.ai/delegator/install.ps1 | iex
```

The installer selects the latest stable release for native Windows amd64 or
arm64 and installs to `%LOCALAPPDATA%\Programs\Delegator\bin`. It updates your
user and current-session PATH without administrator permissions. You'll need
Git and an installed, authenticated coding agent for setup.

On an interactive console, the installer starts `dg init` automatically in the
same terminal. For unattended installation, set `$env:DG_NON_INTERACTIVE = '1'`
before running the command. Non-interactive hosts and redirected console streams
also skip setup and print its command. Cancellation or setup failure leaves `dg`
installed and usable; run `dg init` to retry.

## Options

Use the ScriptBlock form to pass explicit options. This example selects a
release, uses a path containing spaces, and skips setup; replace the example
version with the release you want:

```powershell
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing -Uri 'https://alcubi.ai/delegator/install.ps1').Content)) -Version v1.2.3 -InstallDir "$env:LOCALAPPDATA\Delegator Tools" -NonInteractive
```

| Parameter | Environment default | Behavior |
| --- | --- | --- |
| `-Version` | `DG_VERSION` | Select a release; latest stable if unset. |
| `-InstallDir` | `DG_INSTALL_DIR` | Choose the directory containing `dg.exe`. |
| `-NonInteractive` | `DG_NON_INTERACTIVE` (`0` or `1`) | Skip guided setup. |

Explicit parameters take precedence. `-NonInteractive:$false` overrides the
environment setting. The short `iwr | iex` form uses environment defaults;
pass parameters to the ScriptBlock form instead.

Versions accept an optional leading `v`, prerelease suffix, and build metadata.
Older releases using `alcubi-delegator_` filenames are supported when they
contain Windows binaries and a canonical checksum file.

## Help

To fetch the installer and display help without installing:

```powershell
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing -Uri 'https://alcubi.ai/delegator/install.ps1').Content)) -Help
```

The bootstrap fetches the script, but `-Help` does not query release metadata,
download archives, change PATH, or start setup. With a saved script,
`./install.ps1 -Help` needs no network access.

## Upgrades and failures

Run the installer again to reinstall or upgrade. Close running `dg` processes
first. Downloads and SHA-256 verification finish before replacing `dg.exe`.
The installer keeps a backup until user PATH persistence succeeds, and updates
the current-session PATH only after that succeeds. Failed downloads, checksum
checks, binary replacement, or PATH persistence preserve the previous binary.
Existing PATH entries, including unexpanded variables, are preserved.

If replacement fails, check destination and user registry permissions and close
running `dg` processes before retrying. If rollback itself fails, the error
reports the backup location for recovery. Temporary files are cleaned up;
cleanup failures report the remaining paths.

## Installer validation

With Go and Make installed, run `make install-test-windows` for PowerShell 7 or
`make install-test-windows POWERSHELL=powershell` for Windows PowerShell 5.1.
Both shells run in the Windows validation and release workflows. Linux Go tests
do not require PowerShell.

See the [installer test contract](../scripts/windows-installer.md) for fixture
coverage and manual console validation. The x64 fixture suite checks ARM64 asset
selection and extraction; native ARM64 execution is not tested.
