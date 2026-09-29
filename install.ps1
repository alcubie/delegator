# Windows PowerShell 5.1 and PowerShell 7; run in the current session.
# & ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing -Uri 'https://alcubi.ai/delegator/install.ps1').Content))
[CmdletBinding()]
param(
    [string] $Version,
    [string] $InstallDir,
    [switch] $NonInteractive,
    [switch] $Help
)

if ($Help) {
    Write-Output @'
Usage: ./install.ps1 [-Version VERSION] [-InstallDir DIRECTORY] [-NonInteractive] [-Help]
Installs the latest stable release by default, for native Windows amd64 or arm64.
-Version accepts a release version with optional v prefix (including prereleases).
-InstallDir defaults to %LOCALAPPDATA%\Programs\Delegator\bin.
DG_VERSION, DG_INSTALL_DIR and DG_NON_INTERACTIVE (0 or 1) supply defaults.
Explicit parameters take precedence; -NonInteractive:$false overrides the environment.
The user and current session PATH are updated, without administrator permissions.
On an interactive console, starts dg init; otherwise prints the setup command.
-Help performs no downloads, installation, or setup.
Pass options in your existing PowerShell session (replace the example version):
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing -Uri 'https://alcubi.ai/delegator/install.ps1').Content)) -Version v1.2.3 -InstallDir "$env:LOCALAPPDATA\Delegator Tools" -NonInteractive
Close running dg processes before upgrading. Failed installation preserves an existing binary.
'@
    return
}
$pipelineInput = $MyInvocation.ExpectingInput
if (-not $PSBoundParameters.ContainsKey('Version')) { $Version = $env:DG_VERSION }
if (-not $PSBoundParameters.ContainsKey('InstallDir')) { $InstallDir = $env:DG_INSTALL_DIR }
if (-not $PSBoundParameters.ContainsKey('NonInteractive')) {
    if ($env:DG_NON_INTERACTIVE -and $env:DG_NON_INTERACTIVE -notin @('0', '1')) {
        throw 'DG_NON_INTERACTIVE must be 0 or 1.'
    }
    $NonInteractive = $env:DG_NON_INTERACTIVE -eq '1'
}
& {
    $ErrorActionPreference = 'Stop'
    if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) {
        throw 'The Delegator PowerShell installer requires Windows.'
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
    if (-not $InstallDir) {
        if (-not $env:LOCALAPPDATA) { throw 'LOCALAPPDATA is not set; supply -InstallDir.' }
        $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\Delegator\bin'
    }
    $installDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($InstallDir)
    if ($installDir.Contains(';')) { throw 'InstallDir cannot contain a semicolon because it must be added to PATH.' }
    $versionPattern = '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
    if ($Version) {
        $Version = $Version -creplace '^v', ''
        if ($Version -cnotmatch $versionPattern) { throw "Invalid version: $Version" }
    }
    $destination = Join-Path $installDir 'dg.exe'
    $stage = Join-Path ([IO.Path]::GetTempPath()) ('delegator-install-' + [guid]::NewGuid().ToString('N'))
    $previousProtocol = [Net.ServicePointManager]::SecurityProtocol
    try {
        # Windows PowerShell can otherwise use an older TLS default.
        [Net.ServicePointManager]::SecurityProtocol = $previousProtocol -bor [Net.SecurityProtocolType]::Tls12
        $null = New-Item -ItemType Directory -Path $stage
        if (-not $Version) {
            try {
                $release = (Invoke-WebRequest -UseBasicParsing -Uri 'https://api.github.com/repos/alcubie/delegator/releases/latest').Content | ConvertFrom-Json
                if ($release.draft -or $release.prerelease -or $release.tag_name -cnotmatch '^v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?)$') {
                    throw 'Latest release metadata does not identify a stable version.'
                }
                $Version = $Matches[1]
            } catch { throw "Could not determine the latest stable release. Check network access or use -Version. $($_.Exception.Message)" }
        }
        $base = 'https://github.com/alcubie/delegator/releases/download/' + [Uri]::EscapeDataString("v$Version")
        $checksums = Join-Path $stage 'checksums.txt'
        $prefix = 'delegator'
        try {
            Invoke-WebRequest -UseBasicParsing -Uri "$base/${prefix}_${Version}_checksums.txt" -OutFile $checksums
        } catch {
            $prefix = 'alcubi-delegator'
            try {
                Invoke-WebRequest -UseBasicParsing -Uri "$base/${prefix}_${Version}_checksums.txt" -OutFile $checksums
            } catch { throw "Release v$Version is unavailable or has no canonical checksum file. Check the version and network access. $($_.Exception.Message)" }
        }
        $archiveName = "${prefix}_${Version}_windows_${arch}.zip"
        $archive = Join-Path $stage $archiveName
        try { Invoke-WebRequest -UseBasicParsing -Uri "$base/$archiveName" -OutFile $archive }
        catch { throw "Could not download $archiveName. Check network access and whether release v$Version contains Windows/$arch binaries. $($_.Exception.Message)" }

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

        function Add-InstallPath([string] $Value) {
            foreach ($entry in ($Value -split ';')) {
                $expanded = [Environment]::ExpandEnvironmentVariables($entry.Trim().Trim('"')).TrimEnd('\', '/')
                if ($expanded -ieq $installDir.TrimEnd('\', '/')) { return $Value }
            }
            if ([string]::IsNullOrEmpty($Value)) { return $installDir }
            $separator = if ($Value.EndsWith(';')) { '' } else { ';' }
            return $Value + $separator + $installDir
        }

        # Stage on the destination volume, then atomically replace with a backup.
        # PATH persistence is part of the transaction: a failure restores the EXE.
        $pending = Join-Path $installDir ('.dg-' + [guid]::NewGuid().ToString('N') + '.tmp')
        $backup = $pending + '.bak'
        $replaced = $false
        $hadBinary = Test-Path -LiteralPath $destination
        $userEnvironment = $null
        $pathAttempted = $false
        try {
            $null = New-Item -ItemType Directory -Path $installDir -Force
            Copy-Item -LiteralPath $binary -Destination $pending
            $userEnvironment = Get-Item -LiteralPath 'HKCU:\Environment'
            $hadPath = $userEnvironment.GetValueNames() -contains 'Path'
            $userPath = [string]$userEnvironment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $pathKind = if ($hadPath) { $userEnvironment.GetValueKind('Path') } else { $null }
            $newUserPath = Add-InstallPath $userPath
            if ($hadBinary) { [IO.File]::Replace($pending, $destination, $backup) }
            else { [IO.File]::Move($pending, $destination) }
            $replaced = $true
            if ($newUserPath -cne $userPath) {
                $pathAttempted = $true
                $null = New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name Path -Value $newUserPath -PropertyType ExpandString -Force
                if ($userEnvironment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) -cne $newUserPath) {
                    throw 'The user PATH write did not persist.'
                }
            }
        } catch {
            $failure = $_.Exception.Message
            if ($replaced -or (Test-Path -LiteralPath $backup)) {
                try {
                    if ($hadBinary) {
                        # ReplaceFile can fail after moving the original to backup.
                        if (Test-Path -LiteralPath $destination) { [IO.File]::Replace($backup, $destination, $pending) }
                        else { [IO.File]::Move($backup, $destination) }
                    }
                    else { [IO.File]::Delete($destination) }
                } catch { throw "Installation failed: $failure. Could not restore $destination; recover the previous binary from $backup. $($_.Exception.Message)" }
            }
            if ($pathAttempted) {
                try {
                    if ($hadPath) {
                        $currentPath = $userEnvironment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
                        if ($currentPath -cne $userPath -or $userEnvironment.GetValueKind('Path') -ne $pathKind) {
                            $null = New-ItemProperty -LiteralPath 'HKCU:\Environment' -Name Path -Value $userPath -PropertyType $pathKind -Force
                        }
                    }
                    elseif ($userEnvironment.GetValueNames() -contains 'Path') { Remove-ItemProperty -LiteralPath 'HKCU:\Environment' -Name Path }
                    if ($hadPath) {
                        if ($userEnvironment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) -cne $userPath -or $userEnvironment.GetValueKind('Path') -ne $pathKind) {
                            throw 'The original user PATH could not be restored.'
                        }
                    } elseif ($userEnvironment.GetValueNames() -contains 'Path') { throw 'The new user PATH value could not be removed.' }
                } catch { throw "Installation failed and the binary was restored, but user PATH restoration failed. Check HKCU\Environment\Path. $failure $($_.Exception.Message)" }
            }
            throw "Could not install to $destination. Close running dg processes and check directory and user PATH permissions. $failure"
        } finally {
            if ($null -ne $userEnvironment) { $userEnvironment.Close() }
            if (Test-Path -LiteralPath $pending) { Remove-Item -LiteralPath $pending -Force }
        }
        if (Test-Path -LiteralPath $backup) {
            try { Remove-Item -LiteralPath $backup -Force }
            catch { Write-Warning "Installation completed, but the old backup could not be removed: $backup. $($_.Exception.Message)" }
        }
        $env:Path = Add-InstallPath $env:Path

        # Let Explorer pick up the persistent PATH for subsequently opened shells.
        try {
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
        } catch { Write-Warning "PATH was saved, but Windows could not be notified. Open a new terminal if needed. $($_.Exception.Message)" }
        Write-Host "Installed Delegator v$version at $destination"
        $interactive = -not $NonInteractive -and -not $pipelineInput -and
            $Host.Name -eq 'ConsoleHost' -and -not [Console]::IsInputRedirected -and
            -not [Console]::IsOutputRedirected -and -not [Console]::IsErrorRedirected -and
            -not (@([Environment]::GetCommandLineArgs() | Where-Object { $_ -match '^-(NonI(nteractive)?|noni.*)$' }).Count)
        if ($interactive) {
            try {
                & $destination init
                if ($LASTEXITCODE -ne 0) { throw "dg init exited with code $LASTEXITCODE." }
            } catch {
                Write-Host 'To set up Delegator later, run: dg init'
                throw "Delegator was installed, but setup did not complete. $($_.Exception.Message)"
            }
        } else { Write-Host 'To set up Delegator later, run: dg init' }
    } finally {
        [Net.ServicePointManager]::SecurityProtocol = $previousProtocol
        if (Test-Path -LiteralPath $stage) {
            try { Remove-Item -LiteralPath $stage -Recurse -Force }
            catch { Write-Warning "Could not remove installer temporary files at $stage. $($_.Exception.Message)" }
        }
    }
}
