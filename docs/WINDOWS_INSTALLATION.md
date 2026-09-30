# Windows installation

Run in your current Windows PowerShell 5.1 or PowerShell 7 session:

```powershell
iwr -useb https://alcubi.ai/delegator/install.ps1 | iex
```

Interactive installations start `dg init` automatically in the same terminal.
For unattended Windows installation, set `$env:DG_NON_INTERACTIVE = '1'` before
running the command. Redirected or non-interactive sessions also skip setup
and print the `dg init` command to run later. Cancelling setup leaves `dg`
installed and usable.
