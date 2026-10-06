# Executed by the console runner with real console handles (or redirected pipes).
param([string] $Installer, [string] $Root, [string] $Case)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
$registryPath = 'HKCU:\Software\DelegatorInstallerTest-' + [guid]::NewGuid().ToString('N')
$fixture = @{
    architecture = 9
    failure = $(if ($Case -in @('checksum', 'download')) { $Case } else { '' })
    legacy = $false; pathWrites = 0; downloads = @(); stages = @()
}
. (Join-Path $PSScriptRoot 'test-install-fixtures.ps1')
$originalUserPath = Read-RealUserPath
$ProgressPreference = 'SilentlyContinue'
$caseRoot = Join-Path $Root $Case
try {
    $null = New-Item -ItemType Directory -Path $caseRoot
    $null = New-Item -Path $registryPath -Force
    $env:LOCALAPPDATA = Join-Path $caseRoot 'Local App Data'
    $env:XDG_DATA_HOME = Join-Path $caseRoot 'data'
    $env:USERPROFILE = $caseRoot
    $env:HOME = $caseRoot
    $env:APPDATA = Join-Path $caseRoot 'App Data'
    $env:TEMP = $caseRoot
    $env:TMP = $caseRoot
    $env:DG_VERSION = ''
    $env:DG_INSTALL_DIR = ''
    $env:DG_NON_INTERACTIVE = if ($Case -eq 'optout') { '1' } else { '0' }
    $env:PATH = "$caseRoot;$env:SystemRoot\System32;$env:SystemRoot"
    # Only discover this inert agent; never execute a real agent or use its state.
    Set-Content -LiteralPath (Join-Path $caseRoot 'goose.cmd') -Value '@exit /b 99'
    $installDir = Join-Path $env:LOCALAPPDATA 'Programs\Delegator\bin'
    $destination = Join-Path $installDir 'dg.exe'
    if ($Case -eq 'failure') {
        # Installation succeeds, but opening the onboarding data directory fails.
        Set-Content -LiteralPath $env:XDG_DATA_HOME -Value 'not a directory'
    }
    $line = @(Get-Content -LiteralPath (Join-Path (Split-Path $PSScriptRoot -Parent) 'README.md') | Where-Object { $_.StartsWith('iwr ') })
    Assert ($line.Count -eq 1) 'Missing README command'
    $message = ''
    try { & ([scriptblock]::Create($line[0])) } catch { $message = $_.Exception.Message }
    Assert-CleanStaging
    if ($Case -in @('checksum', 'download')) {
        $expected = if ($Case -eq 'checksum') { '*Checksum verification failed*' } else { '*Fixture download failure*' }
        Assert ($message -like $expected) "Unexpected installation failure: $message"
        Assert (-not (Test-Path -LiteralPath $destination)) 'Failed installation wrote a binary'
    } else {
        Assert ($message -eq '') "Installer threw: $message"
        Assert ((Get-Command dg -CommandType Application).Source -eq $destination) 'Current-session PATH did not resolve installed dg'
        Assert ((Read-UserPath) -eq $installDir) 'Persistent user PATH was not updated'
        $version = & $destination version
        Assert ($LASTEXITCODE -eq 0 -and $version -eq 'dg v1.2.3') 'Installed binary is unusable'
        if ($Case -in @('select', 'cancel')) {
            $selected = & $destination config get default_agent
            Assert ($LASTEXITCODE -eq 0) 'Cannot read onboarding configuration'
            if ($Case -eq 'select') {
                Assert ($selected -eq 'goose') 'Keyboard selection was not saved'
                $telemetry = & $destination config get telemetry
                Assert ($LASTEXITCODE -eq 0) 'Cannot read telemetry configuration'
                Assert ($telemetry -eq 'false') 'Keyboard telemetry opt-out was not saved'
            }
            else { Assert ([string]::IsNullOrEmpty($selected)) 'Cancellation selected an agent' }
        }
    }
    if ($Case -in @('optout', 'noninteractive', 'redirected', 'checksum', 'download')) {
        Assert (-not (Test-Path -LiteralPath $env:XDG_DATA_HOME)) 'Unattended or failed installation started onboarding'
    }
    Write-Host "SESSION PASS: $Case"
} finally {
    if (Test-Path -LiteralPath $registryPath) { Remove-Item -LiteralPath $registryPath -Recurse -Force }
    Assert ((Read-RealUserPath) -ceq $originalUserPath) 'Real user PATH changed'
}
