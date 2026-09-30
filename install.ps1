# Windows PowerShell 5.1 and PowerShell 7. Run in the current process so PATH
# is immediately available. The public download-and-run command is:
# iwr -useb https://alcubi.ai/delegator/install.ps1 | iex
& {
    $ErrorActionPreference = 'Stop'
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'The Delegator PowerShell installer requires Windows.'
    }
    if ($env:DG_NON_INTERACTIVE -and $env:DG_NON_INTERACTIVE -notin @('0', '1')) {
        throw 'DG_NON_INTERACTIVE must be 0 or 1.'
    }

    # Query the platform, not the architecture of an emulated shell process.
    $processor = Get-CimInstance -ClassName Win32_Processor | Select-Object -First 1
    $arch = switch ($processor.Architecture) {
        9 { 'amd64' }
        12 { 'arm64' }
        default { throw "Unsupported Windows architecture: $($processor.Architecture). Expected amd64 or arm64." }
    }
    if (-not [Environment]::Is64BitOperatingSystem) {
        throw 'Delegator requires 64-bit Windows (amd64 or arm64).'
    }
    if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set.' }
    $installDir = Join-Path $env:LOCALAPPDATA 'Programs\Delegator\bin'
    $destination = Join-Path $installDir 'dg.exe'
    $stage = Join-Path ([IO.Path]::GetTempPath()) ('delegator-install-' + [guid]::NewGuid().ToString('N'))
    $previousProtocol = [Net.ServicePointManager]::SecurityProtocol
    try {
        # Windows PowerShell can otherwise use an older TLS default.
        [Net.ServicePointManager]::SecurityProtocol = $previousProtocol -bor [Net.SecurityProtocolType]::Tls12
        $null = New-Item -ItemType Directory -Path $stage
        $release = (Invoke-WebRequest -UseBasicParsing -Uri 'https://api.github.com/repos/alcubie/delegator/releases/latest').Content | ConvertFrom-Json
        if ($release.draft -or $release.prerelease -or $release.tag_name -cnotmatch '^v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?)$') {
            throw 'Latest release metadata does not identify a stable version.'
        }
        $version = $Matches[1]
        $archiveName = "delegator_${version}_windows_${arch}.zip"
        $base = 'https://github.com/alcubie/delegator/releases/download/' + [Uri]::EscapeDataString($release.tag_name)
        $archive = Join-Path $stage $archiveName
        $checksums = Join-Path $stage 'checksums.txt'
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$archiveName" -OutFile $archive
        Invoke-WebRequest -UseBasicParsing -Uri "$base/delegator_${version}_checksums.txt" -OutFile $checksums

        $pattern = '^([0-9a-fA-F]{64})\s+\*?' + [regex]::Escape($archiveName) + '$'
        $hashes = @(Get-Content -LiteralPath $checksums | ForEach-Object {
            if ($_ -match $pattern) { $Matches[1] }
        })
        if ($hashes.Count -ne 1) { throw "Canonical checksum file has no unique valid entry for $archiveName." }
        if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ne $hashes[0]) {
            throw "Checksum verification failed for $archiveName."
        }
        $extract = Join-Path $stage 'extract'
        Expand-Archive -LiteralPath $archive -DestinationPath $extract
        $binary = Join-Path $extract 'dg.exe'
        if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw 'Release archive does not contain dg.exe.' }
        $null = New-Item -ItemType Directory -Path $installDir -Force
        Copy-Item -LiteralPath $binary -Destination $destination -Force

        function Add-InstallPath([string] $Value) {
            foreach ($entry in ($Value -split ';')) {
                $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim().Trim('"')).TrimEnd('\', '/')
                if ($expanded -ieq $installDir.TrimEnd('\', '/')) { return $Value }
            }
            if ([string]::IsNullOrEmpty($Value)) { return $installDir }
            $separator = if ($Value.EndsWith(';')) { '' } else { ';' }
            return $Value + $separator + $installDir
        }

        # Read the raw registry value to preserve variables such as %USERPROFILE%.
        # Only the user PATH is persisted; never copy the combined process PATH.
        $userEnvironment = Get-Item -LiteralPath 'HKCU:\Environment'
        try {
            $userPath = [string]$userEnvironment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $newUserPath = Add-InstallPath $userPath
            if ($newUserPath -cne $userPath) {
                $null = New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name Path -Value $newUserPath -PropertyType ExpandString -Force
            }
        } finally {
            $userEnvironment.Close()
        }
        $env:Path = Add-InstallPath $env:Path

        # Let Explorer pick up the persistent PATH for subsequently opened shells.
        if (-not ('Delegator.InstallerEnvironment' -as [type])) {
            Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
namespace Delegator {
    public static class InstallerEnvironment {
        [DllImport("user32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        public static extern IntPtr SendMessageTimeout(IntPtr window, uint message,
            UIntPtr wParam, string lParam, uint flags, uint timeout, out UIntPtr result);
    }
}
'@
        }
        $result = [UIntPtr]::Zero
        $null = [Delegator.InstallerEnvironment]::SendMessageTimeout([IntPtr]0xffff, 0x001a, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$result)
        Write-Host "Installed Delegator v$version at $destination"
    } finally {
        [Net.ServicePointManager]::SecurityProtocol = $previousProtocol
        if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
    }

    # The web response is PowerShell pipeline input, not keyboard input. Require
    # a console on all OS streams and respect the shell's unattended mode too.
    $unattendedShell = @([Environment]::GetCommandLineArgs() | Where-Object {
        $_ -match '^-noni'
    }).Count -gt 0
    if ($env:DG_NON_INTERACTIVE -eq '1' -or $unattendedShell -or
        -not [Environment]::UserInteractive -or $Host.Name -ne 'ConsoleHost' -or
        [Console]::IsInputRedirected -or [Console]::IsOutputRedirected -or [Console]::IsErrorRedirected) {
        Write-Host 'To set up Alcubi Delegator later, run:'
        Write-Host '    dg init'
        return
    }

    $setup = $null
    $setupExited = $false
    try {
        # Inherit the console directly, bypassing native-command pipeline input
        # from iwr | iex. FileName is a resolved path, not a quoted command line.
        $start = New-Object System.Diagnostics.ProcessStartInfo
        $start.FileName = $destination
        $start.Arguments = 'init'
        $start.WorkingDirectory = $PWD.Path
        $start.UseShellExecute = $false
        $setup = [Diagnostics.Process]::Start($start)
        $setup.WaitForExit()
        $setupExited = $setup.ExitCode -eq 0
        if (-not $setupExited) {
            Write-Host "Setup exited with code $($setup.ExitCode)."
        }
    } catch {
        Write-Host "Could not complete setup: $($_.Exception.Message)"
    } finally {
        if ($null -ne $setup) { $setup.Dispose() }
        if (-not $setupExited) {
            Write-Host 'Delegator is installed, but setup is incomplete.'
        }
        # dg init also reports voluntary cancellation (which exits successfully).
        Write-Host 'To run setup again later: dg init'
    }
}
