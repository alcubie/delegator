# Windows installer contract

`install.ps1` installs the latest stable release for native Windows amd64 or
arm64 to `%LOCALAPPDATA%\Programs\Delegator\bin`. It verifies the release ZIP
against `delegator_<version>_checksums.txt` before extraction. It adds that
directory to the user and current process PATH, then prints `dg version` and
`dg init`. It does not prompt or start setup.

The downstream hosting/docs ticket should publish the script at the URL below.
**This URL is a future hosting contract, not a currently available installer.**
The exact command is also recorded in `install.ps1` and executed with controlled
downloads by the Windows smoke test, alongside the current GitHub release asset
command from the README:

```powershell
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing -Uri 'https://alcubi.ai/delegator/install.ps1').Content))
```

Run it in the existing PowerShell session. ScriptBlock invocation preserves
current-session environment changes and requires no permanent execution-policy
change. `-UseBasicParsing` avoids Internet Explorer dependencies and web content
script prompts in Windows PowerShell 5.1. Do not wrap the command in a child
`powershell.exe` or `pwsh` process when documenting immediate command discovery.

The `Windows installer` workflow runs the actual installer under Windows
PowerShell 5.1 and PowerShell 7 on an x64 Windows runner. It builds real Windows
release fixtures, redirects downloads and the registry key to isolated data,
resolves `dg`, and runs `dg version`. It also checks repeated PATH updates,
checksum/download failure preservation, staging cleanup, and ARM64 asset
selection/extraction. It does not claim native ARM64 execution or test live
hosting. The developer's real user PATH is never written.

For local smoke checks on Windows with Go and Make installed, run
`make install-test-windows` (PowerShell 7) or
`make install-test-windows POWERSHELL=powershell` (Windows PowerShell 5.1).
The test's execution-policy override applies only to its child process. The
installer itself requires neither Go nor Make. Version/directory switches,
guided setup, and the larger failure matrix remain deferred.
