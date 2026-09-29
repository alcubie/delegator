# Windows installer contract

`install.ps1` supports Windows PowerShell 5.1 and PowerShell 7 on native Windows
amd64 or arm64. It defaults to the latest stable release and
`%LOCALAPPDATA%\Programs\Delegator\bin`, verifies the ZIP against the canonical
SHA-256 checksum file, and updates only the user and current process PATH.
Existing PATH entries, including unexpanded variables, are preserved.

Run the bootstrap in the existing PowerShell session for immediate command
availability. The downstream website ticket owns the public redirect and guide;
the README retains the direct GitHub asset URL until that deployment is verified.
The website contract is:

```powershell
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing -Uri 'https://alcubi.ai/delegator/install.ps1').Content))
```

Append `-Version v1.2.3 -InstallDir "$env:LOCALAPPDATA\Delegator Tools" -NonInteractive`
to select a release and directory and skip setup. Both README bootstrap forms
are executed against fixture downloads by the tests. `DG_VERSION`,
`DG_INSTALL_DIR`, and `DG_NON_INTERACTIVE` (0 or 1) supply defaults; explicit
parameters win, including `-NonInteractive:$false`. An optional leading `v`,
prerelease suffix, and build metadata are accepted. The historical
`alcubi-delegator_` asset prefix is supported when that release has Windows ZIPs.
`-Help` returns before platform checks, downloads, installation, or setup.

An interactive console starts `dg init`. Non-interactive mode, a non-interactive
PowerShell host, or redirected console input/output prints the command instead.
Setup failure reports how to retry and keeps the completed installation.
Downloads and extraction finish before installation. An adjacent staged file
replaces the binary with a backup; failed user PATH persistence restores that
backup. Locked binaries and unwritable directories produce actionable errors.
Close running `dg` processes before upgrading. A failed rollback reports the
backup location for recovery rather than discarding it.

The existing Windows installer and release workflows run fixture-backed checks
under both PowerShell versions on x64 Windows, with a 20-minute job timeout.
They build real binaries for two versions and check:

- Exact bootstrap commands, option/environment precedence, help with no downloads,
  version rejection, latest stable selection, and historical asset names.
- Install/reinstall/upgrade, immediate command discovery, persisted PATH,
  equivalent PATH entries, spaces, and temporary-file cleanup.
- Invalid, missing and duplicate checksums, missing binaries, API/download errors,
  a locked destination, real ACL denial, and failed/partial/silent PATH writes.
- Preservation of the old executable and PATH, including rollback of an attempted
  upgrade to different bytes and rollback of a failed first installation.
- Interactive onboarding with a harmless fixture EXE in a fresh hidden Windows
  console, plus switch/environment/host suppression and redirected input. Each
  child has a 30-second timeout and records whether `init` actually ran.

All downloads and registry writes use isolated fixtures; onboarding never runs
real user setup. ARM64 asset selection and extraction are checked on x64;
**native ARM64 execution is not tested**. The tests do not contact mutable releases
or verify live hosting. Run `make install-test-windows` for PowerShell 7 or
`make install-test-windows POWERSHELL=powershell` for Windows PowerShell 5.1.
Linux Go tests do not require either shell.

For optional manual validation of the actual setup UI, use a disposable Windows
account with Git and an authenticated agent. In each supported shell, run the
README bootstrap without options and complete `dg init`; verify `dg version`
works in that session and a new terminal. Repeat with `-NonInteractive` and
confirm only the setup command is printed. Keep `dg` running and attempt an
upgrade, then stop it and retry; verify the installed version after each attempt.
This manual UI exercise and native Windows runs were not available on the Linux
ticket host; the automated native checks must pass in CI before release.
