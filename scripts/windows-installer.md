# Windows installer contract

`install.ps1` installs the latest stable release for native Windows amd64 or
arm64 to `%LOCALAPPDATA%\Programs\Delegator\bin`. It verifies the release ZIP
against `delegator_<version>_checksums.txt` before extraction. It adds that
directory to the user and current process PATH, then automatically starts the
installed `dg init` in the same terminal. Onboarding owns all setup prompts.
The executable's resolved path is used, including when it contains spaces.

The public URL below redirects to the latest stable GitHub release's
`install.ps1` asset. The exact README command is also recorded in `install.ps1`
and executed with controlled downloads by the Windows smoke test:

```powershell
iwr -useb https://alcubi.ai/delegator/install.ps1 | iex
```

Run it in the existing PowerShell session. `iwr` and `iex` are aliases for
`Invoke-WebRequest` and `Invoke-Expression`. This preserves current-session
environment changes and requires no permanent execution-policy change.
`-useb` abbreviates `-UseBasicParsing`, avoiding Internet Explorer dependencies and web content
script prompts in Windows PowerShell 5.1. Do not wrap the command in a child
`powershell.exe` or `pwsh` process when documenting immediate command discovery.

Set `$env:DG_NON_INTERACTIVE = '1'` before installation to skip onboarding
(the same environment opt-out as the shell installer). Unset it or set it to
`'0'` to allow automatic setup again. Other nonempty values are rejected.
PowerShell's `-NonInteractive` mode, non-console hosts, and redirected standard
input, output, or error also skip setup and print the manual `dg init` next step.
Installation success is reported before setup starts. Cancellation or setup
failure leaves the binary and PATH installed; run `dg init` again to finish.
No setup is launched if downloading or checksum verification fails.

The `Windows installer` workflow runs the actual installer under Windows
PowerShell 5.1 and PowerShell 7 on an x64 Windows runner. It builds real Windows
release fixtures, redirects downloads and the registry key to isolated data,
resolves `dg`, and runs `dg version`. It also checks repeated PATH updates,
checksum/download failure preservation, staging cleanup, and ARM64 asset
selection/extraction. A ConPTY console drives the exact README command and the
real onboarding selector with keyboard input, checking saved selection,
cancellation, setup failure, explicit opt-out, PowerShell non-interactive mode,
and failed downloads/checksums. A redirected process checks unattended setup.
Each session isolates application data, home, agent discovery, and registry
state, and verifies that the installed executable still works. No live release
or real agent is used. These checks do not claim native ARM64 execution or test
live hosting. The developer's real user PATH is never written.

For local smoke checks on Windows with Go and Make installed, run
`make install-test-windows` (PowerShell 7) or
`make install-test-windows POWERSHELL=powershell` (Windows PowerShell 5.1).
The test's execution-policy override applies only to its child process. The
installer itself requires neither Go nor Make. The console tests require
Windows 10 version 1809 or later (ConPTY); this is a test-harness requirement.
Version/directory switches and the larger failure matrix remain deferred.
