# Runs on Windows, using real dg binaries and isolated filesystem/registry data.
# Network and user-environment access are redirected below, without Pester.
param([string] $InstallerPath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'install.ps1'))

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Run this smoke test on Windows.' }
$repo = Split-Path $PSScriptRoot -Parent
$installer = (Resolve-Path -LiteralPath $InstallerPath).Path
$root = Join-Path $repo ('.installer-test-' + [guid]::NewGuid().ToString('N'))
$registryPath = 'HKCU:\Software\DelegatorInstallerTest-' + [guid]::NewGuid().ToString('N')
$saved = @{}
foreach ($name in @('LOCALAPPDATA', 'PATH', 'TEMP', 'TMP', 'GOOS', 'GOARCH', 'CGO_ENABLED')) {
    $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
$originalUserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$script:architecture = $null
$script:badChecksum = $false
$script:failDownload = $false
$script:downloads = @()
$script:stages = @()

function Assert($condition, [string] $message) {
    if (-not $condition) { throw $message }
}

# These proxies allow the actual installer to read and write a real registry
# value, but never the developer's HKCU:\Environment or application directory.
function Get-Item {
    param([string] $LiteralPath)
    Assert ($LiteralPath -eq 'HKCU:\Environment') "Unexpected registry read: $LiteralPath"
    Microsoft.PowerShell.Management\Get-Item -LiteralPath $registryPath
}
function New-ItemProperty {
    param([string] $LiteralPath, [string] $Name, [string] $Value, [string] $PropertyType, [switch] $Force)
    Assert ($LiteralPath -eq 'HKCU:\Environment' -and $Name -eq 'Path') 'Unexpected registry write'
    Microsoft.PowerShell.Management\New-ItemProperty -LiteralPath $registryPath -Name $Name -Value $Value -PropertyType $PropertyType -Force
}
function Get-CimInstance {
    param([string] $ClassName)
    Assert ($ClassName -eq 'Win32_Processor') 'Unexpected CIM query'
    if ($null -ne $script:architecture) { return [pscustomobject]@{ Architecture = $script:architecture } }
    CimCmdlets\Get-CimInstance -ClassName $ClassName
}
function Invoke-WebRequest {
    param([string] $Uri, [string] $OutFile, [switch] $UseBasicParsing)
    Assert $UseBasicParsing 'Downloads must use basic parsing on Windows PowerShell'
    $script:downloads += $Uri
    if ($Uri -in @('https://alcubi.ai/delegator/install.ps1', 'https://github.com/alcubie/delegator/releases/latest/download/install.ps1')) {
        return [pscustomobject]@{ Content = [IO.File]::ReadAllText($installer) }
    }
    if ($Uri -eq 'https://api.github.com/repos/alcubie/delegator/releases/latest') {
        return [pscustomobject]@{ Content = '{"tag_name":"v1.2.3","draft":false,"prerelease":false}' }
    }
    Assert ($Uri.StartsWith('https://github.com/alcubie/delegator/releases/download/v1.2.3/')) "Unexpected release URL: $Uri"
    $script:stages += Split-Path $OutFile -Parent
    if ($script:failDownload) { throw 'Fixture download failure' }
    $name = ($Uri -split '/')[-1]
    if ($script:badChecksum -and $name -eq 'delegator_1.2.3_checksums.txt') {
        $invalid = @('amd64', 'arm64') | ForEach-Object { ('0' * 64) + "  delegator_1.2.3_windows_$_.zip" }
        Set-Content -LiteralPath $OutFile -Value $invalid
    } else {
        Copy-Item -LiteralPath (Join-Path $root $name) -Destination $OutFile
    }
}
function Read-UserPath {
    $key = Microsoft.PowerShell.Management\Get-Item -LiteralPath $registryPath
    try { $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) }
    finally { $key.Close() }
}
function Assert-CleanStaging {
    foreach ($stage in $script:stages) { Assert (-not (Test-Path -LiteralPath $stage)) "Staging directory leaked: $stage" }
}
function Assert-Version {
    $command = Get-Command dg -CommandType Application
    Assert ($command.Source -eq $destination) "Resolved the wrong dg: $($command.Source)"
    $version = dg version
    Assert ($LASTEXITCODE -eq 0 -and $version -eq 'dg v1.2.3') "Unexpected version: $version"
}

try {
    $null = New-Item -ItemType Directory -Path $root
    $null = New-Item -Path $registryPath -Force
    $checksums = @()
    Push-Location $repo
    try {
        $env:GOOS = 'windows'
        $env:CGO_ENABLED = '0'
        foreach ($arch in @('amd64', 'arm64')) {
            $env:GOARCH = $arch
            $payload = Join-Path $root $arch
            $null = New-Item -ItemType Directory -Path $payload
            go build -ldflags '-X github.com/alcubie/delegator/internal/cli.Version=v1.2.3' -o (Join-Path $payload 'dg.exe') ./cmd/dg
            Assert ($LASTEXITCODE -eq 0) "Could not build $arch fixture"
            $name = "delegator_1.2.3_windows_${arch}.zip"
            $zip = Join-Path $root $name
            Compress-Archive -LiteralPath (Join-Path $payload 'dg.exe') -DestinationPath $zip
            $checksums += (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash + '  ' + $name
        }
    } finally { Pop-Location }
    Set-Content -LiteralPath (Join-Path $root 'delegator_1.2.3_checksums.txt') -Value $checksums
    $env:LOCALAPPDATA = Join-Path $root 'Local App Data'
    $env:TEMP = $root
    $env:TMP = $root
    $installDir = Join-Path $env:LOCALAPPDATA 'Programs\Delegator\bin'
    $destination = Join-Path $installDir 'dg.exe'
    # Use Windows directories for shell commands; exclude any preinstalled dg.
    $baseProcessPath = "$env:SystemRoot\System32;$env:SystemRoot"
    $env:PATH = $baseProcessPath
    $baseUserPath = '%USERPROFILE%\Existing Tools;C:\Other Tools'
    $null = Microsoft.PowerShell.Management\New-ItemProperty -LiteralPath $registryPath -Name Path -Value $baseUserPath -PropertyType ExpandString

    # Execute the README's current GitHub command and the future website form.
    $readmeLine = @(Get-Content -LiteralPath (Join-Path $repo 'README.md') | Where-Object { $_.StartsWith('& ([scriptblock]') })
    Assert ($readmeLine.Count -eq 1) 'Missing README download-and-run command'
    & ([scriptblock]::Create($readmeLine[0]))
    Assert-Version
    Assert-CleanStaging

    # Execute the exact future download-and-run form recorded in the installer.
    # ScriptBlock invocation stays in this process and bypasses no policy setting.
    $line = (Get-Content -LiteralPath $installer | Where-Object { $_.StartsWith('# & ([scriptblock]') })
    Assert (@($line).Count -eq 1) 'Missing download-and-run contract'
    $bootstrap = [scriptblock]::Create($line.Substring(2))
    & $bootstrap
    Assert-Version
    Assert ((Read-UserPath) -ceq "$baseUserPath;$installDir") 'Existing user PATH entries changed'
    Assert ($env:PATH -ceq "$baseProcessPath;$installDir") 'Current process PATH was not updated'
    Assert-CleanStaging

    & $bootstrap
    Assert-Version
    Assert ((Read-UserPath) -ceq "$baseUserPath;$installDir") 'Repeat install duplicated user PATH'
    Assert ($env:PATH -ceq "$baseProcessPath;$installDir") 'Repeat install duplicated process PATH'
    # Case, quotes, environment variables and trailing separators are equivalent.
    $equivalent = '%LOCALAPPDATA%\Programs\Delegator\bin\'
    $null = Microsoft.PowerShell.Management\Set-ItemProperty -LiteralPath $registryPath -Name Path -Value "$baseUserPath;$equivalent"
    $env:PATH = "$baseProcessPath;`"$($installDir.ToUpperInvariant())\`""
    $equivalentProcessPath = $env:PATH
    & $bootstrap
    Assert ((Read-UserPath) -ceq "$baseUserPath;$equivalent") 'Equivalent user PATH entry duplicated'
    Assert ($env:PATH -ceq $equivalentProcessPath) 'Equivalent process PATH entry duplicated'

    # Model the environment inherited by a newly opened terminal from persisted
    # user PATH, without carrying over the installer's current-process changes.
    $env:PATH = $baseProcessPath + ';' + [Environment]::ExpandEnvironmentVariables((Read-UserPath))
    Assert-Version
    $beforeHash = (Get-FileHash -LiteralPath $destination).Hash
    $beforeUserPath = Read-UserPath
    $beforeProcessPath = $env:PATH
    foreach ($failure in @('checksum', 'download')) {
        $script:badChecksum = $failure -eq 'checksum'
        $script:failDownload = $failure -eq 'download'
        $message = ''
        try { & $bootstrap } catch { $message = $_.Exception.Message }
        $expected = if ($failure -eq 'checksum') { '*Checksum verification failed*' } else { '*Fixture download failure*' }
        Assert ($message -like $expected) "Wrong $failure failure: $message"
        Assert ((Get-FileHash -LiteralPath $destination).Hash -eq $beforeHash) "$failure replaced the existing binary"
        Assert ((Read-UserPath) -ceq $beforeUserPath -and $env:PATH -ceq $beforeProcessPath) "$failure modified PATH"
        Assert-CleanStaging
    }
    $script:badChecksum = $false
    $script:failDownload = $false
    # ARM selection/extraction only: this x64 runner never executes the ARM EXE.
    $script:architecture = 12
    & $bootstrap
    Assert ($script:downloads -contains 'https://github.com/alcubie/delegator/releases/download/v1.2.3/delegator_1.2.3_windows_arm64.zip') 'ARM64 asset was not selected'
    Assert ((Get-FileHash -LiteralPath $destination).Hash -eq (Get-FileHash -LiteralPath (Join-Path $root 'arm64\dg.exe')).Hash) 'ARM64 payload was not installed'
    Assert-CleanStaging
    $script:architecture = 0
    $message = ''
    try { & $bootstrap } catch { $message = $_.Exception.Message }
    Assert ($message -like '*Unsupported Windows architecture*') 'Unsupported architecture was not rejected'
    Write-Host "Installer smoke checks passed on PowerShell $($PSVersionTable.PSVersion). ARM64 selection checked; ARM64 execution not tested."
} finally {
    foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
    if (Test-Path -LiteralPath $registryPath) { Remove-Item -LiteralPath $registryPath -Recurse -Force }
    Assert ([Environment]::GetEnvironmentVariable('Path', 'User') -ceq $originalUserPath) 'The real user PATH changed'
}
