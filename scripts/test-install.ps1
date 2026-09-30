# Runs on Windows, using real dg binaries and isolated filesystem/registry data.
# Network and user-environment access are redirected below, without Pester.
param(
    [string] $InstallerPath = (Join-Path (Split-Path $PSScriptRoot -Parent) 'install.ps1'),
    [ValidateSet('', 'interactive', 'switch', 'environment', 'host', 'redirected')]
    [string] $Scenario = '',
    [string] $FixtureRoot
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2.0
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Run this smoke test on Windows.' }
$repo = Split-Path $PSScriptRoot -Parent
$installer = (Resolve-Path -LiteralPath $InstallerPath).Path
$root = if ($Scenario) { $FixtureRoot } else { Join-Path $repo ('.installer-test-' + [guid]::NewGuid().ToString('N')) }
$registryPath = 'HKCU:\Software\DelegatorInstallerTest-' + [guid]::NewGuid().ToString('N')
$saved = @{}
foreach ($name in @('LOCALAPPDATA', 'PATH', 'TEMP', 'TMP', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'DG_VERSION', 'DG_INSTALL_DIR', 'DG_NON_INTERACTIVE', 'DG_TEST_INIT_LOG', 'XDG_DATA_HOME')) {
    $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
$fixture = @{
    architecture = $null; failure = ''; legacy = $false; pathWrites = 0
    downloads = @(); stages = @()
}

. (Join-Path $PSScriptRoot 'test-install-fixtures.ps1')
$originalUserPath = Read-RealUserPath
function Assert-Version([string] $Expected = 'dg v1.2.3') {
    $command = Get-Command dg -CommandType Application
    Assert ($command.Source -eq $destination) "Resolved the wrong dg: $($command.Source)"
    $version = dg version
    Assert ($LASTEXITCODE -eq 0 -and $version -eq $Expected) "Unexpected version: $version"
}

# A fresh console child runs a harmless EXE that records init, never real setup.
# Redirected children use real OS pipe handles, not mocked terminal detection.
if ($Scenario) {
    try {
        $null = New-Item -Path $registryPath -Force
        $env:LOCALAPPDATA = Join-Path $root $Scenario
        $env:TEMP = $root
        $env:TMP = $root
        $env:DG_VERSION = '1.2.3'
        $env:DG_INSTALL_DIR = ''
        $env:DG_NON_INTERACTIVE = if ($Scenario -in @('interactive', 'environment')) { '1' } else { '0' }
        $env:DG_TEST_INIT_LOG = Join-Path $root "$Scenario.init"
        $parameters = @{}
        if ($Scenario -eq 'interactive') { $parameters.NonInteractive = $false }
        if ($Scenario -eq 'switch') { $parameters.NonInteractive = $true }
        $output = & $installer @parameters 6>&1 | Out-String
        Assert ((Test-Path -LiteralPath $env:DG_TEST_INIT_LOG) -eq ($Scenario -eq 'interactive')) "Unexpected setup behavior for $Scenario"
        if ($Scenario -ne 'interactive') { Assert ($output -match 'dg init') 'Missing setup command' }
        Assert-CleanStaging
        Set-Content -LiteralPath (Join-Path $root "$Scenario.result") -Value 'passed'
    } catch {
        Set-Content -LiteralPath (Join-Path $root "$Scenario.result") -Value ($_ | Out-String)
        throw
    } finally {
        if (Test-Path -LiteralPath $registryPath) { Remove-Item -LiteralPath $registryPath -Recurse -Force }
        Assert ((Read-RealUserPath) -ceq $originalUserPath) 'Child changed real user PATH'
    }
    return
}

try {
    $null = New-Item -ItemType Directory -Path $root
    $null = New-Item -Path $registryPath -Force
    $shellPath = (Get-Process -Id $PID).Path
    $consoleRunner = Join-Path $root 'console-check.exe'
    $env:XDG_DATA_HOME = Join-Path $root 'data'
    Push-Location $repo
    try {
        $env:GOOS = 'windows'
        $env:CGO_ENABLED = '0'
        $env:GOARCH = 'amd64'
        go test ./internal/store -run '^TestDSNWindowsDrivePath$'
        Assert ($LASTEXITCODE -eq 0) 'Windows database URL regression check failed'
        go build -o $consoleRunner ./scripts/windows-installer-console
        Assert ($LASTEXITCODE -eq 0) 'Could not build console test runner'
        foreach ($fixtureVersion in @('1.2.3', '1.2.4')) {
            $checksums = @()
            foreach ($arch in @('amd64', 'arm64')) {
                $env:GOARCH = $arch
                $payload = Join-Path $root "$fixtureVersion-$arch"
                $null = New-Item -ItemType Directory -Path $payload
                go build -ldflags "-X github.com/alcubie/delegator/internal/cli.Version=v$fixtureVersion" -o (Join-Path $payload 'dg.exe') ./cmd/dg
                Assert ($LASTEXITCODE -eq 0) "Could not build $arch fixture"
                $name = "delegator_${fixtureVersion}_windows_${arch}.zip"
                $zip = Join-Path $root $name
                Compress-Archive -LiteralPath (Join-Path $payload 'dg.exe') -DestinationPath $zip
                $checksums += (Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash + '  ' + $name
            }
            Set-Content -LiteralPath (Join-Path $root "delegator_${fixtureVersion}_checksums.txt") -Value $checksums
        }
        $consoleRoot = Join-Path $root 'console fixtures'
        $null = New-Item -ItemType Directory -Path $consoleRoot
        $source = Join-Path $consoleRoot 'main.go'
        Set-Content -LiteralPath $source -Value @'
package main
import ("os"; "fmt")
func main() {
    if len(os.Args) == 2 && os.Args[1] == "init" {
        if err := os.WriteFile(os.Getenv("DG_TEST_INIT_LOG"), []byte("init"), 0600); err != nil { panic(err) }
        return
    }
    fmt.Println("dg v1.2.3")
}
'@
        $env:GOARCH = 'amd64'
        go build -o (Join-Path $consoleRoot 'dg.exe') $source
        Assert ($LASTEXITCODE -eq 0) 'Could not build console fixture'
        $consoleZip = Join-Path $consoleRoot 'delegator_1.2.3_windows_amd64.zip'
        Compress-Archive -LiteralPath (Join-Path $consoleRoot 'dg.exe') -DestinationPath $consoleZip
        Set-Content -LiteralPath (Join-Path $consoleRoot 'delegator_1.2.3_checksums.txt') -Value ((Get-FileHash -LiteralPath $consoleZip).Hash + '  delegator_1.2.3_windows_amd64.zip')
    } finally { Pop-Location }
    & $consoleRunner $shellPath (Join-Path $PSScriptRoot 'test-install-session.ps1') $installer $root
    Assert ($LASTEXITCODE -eq 0) 'Console installer checks failed'
    # Historical names refer to the exact same controlled payload bytes.
    foreach ($arch in @('amd64', 'arm64')) {
        Copy-Item -LiteralPath (Join-Path $root "delegator_1.2.3_windows_$arch.zip") -Destination (Join-Path $root "alcubi-delegator_1.2.3_windows_$arch.zip")
    }
    $legacyChecksums = (Get-Content -LiteralPath (Join-Path $root 'delegator_1.2.3_checksums.txt')) -replace 'delegator_', 'alcubi-delegator_'
    Set-Content -LiteralPath (Join-Path $root 'alcubi-delegator_1.2.3_checksums.txt') -Value $legacyChecksums
    foreach ($arch in @('amd64', 'arm64')) {
        Copy-Item -LiteralPath (Join-Path $root "delegator_1.2.3_windows_$arch.zip") -Destination (Join-Path $root "delegator_1.2.3-rc.1+build.7_windows_$arch.zip")
    }
    $prereleaseChecksums = (Get-Content -LiteralPath (Join-Path $root 'delegator_1.2.3_checksums.txt')) -replace '1.2.3_', '1.2.3-rc.1+build.7_'
    Set-Content -LiteralPath (Join-Path $root 'delegator_1.2.3-rc.1+build.7_checksums.txt') -Value $prereleaseChecksums
    Set-Content -LiteralPath (Join-Path $root 'README.txt') -Value 'Missing dg.exe fixture'
    Compress-Archive -LiteralPath (Join-Path $root 'README.txt') -DestinationPath (Join-Path $root 'empty.zip')
    $env:DG_VERSION = ''
    $env:DG_INSTALL_DIR = ''
    $env:DG_NON_INTERACTIVE = '1'
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

    # Execute the exact README command with controlled public-URL downloads.
    # iwr | iex stays in this process and bypasses no policy setting.
    $readmeLine = @(Get-Content -LiteralPath (Join-Path $repo 'README.md') | Where-Object { $_.StartsWith('iwr ') })
    Assert ($readmeLine.Count -eq 1) 'Missing README download-and-run command'
    # Keep the installer's documented invocation identical to the README.
    $line = (Get-Content -LiteralPath $installer | Where-Object { $_.StartsWith('# iwr ') })
    Assert (@($line).Count -eq 1) 'Missing download-and-run contract'
    Assert ($readmeLine[0] -ceq $line.Substring(2)) 'README command differs from the installer contract'
    $bootstrap = [scriptblock]::Create($readmeLine[0])
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
    foreach ($failure in @('checksum', 'download', 'api', 'metadata', 'archive', 'missing-checksum', 'malformed', 'duplicate', 'missing-binary', 'locked', 'path', 'path-silent', 'path-partial')) {
        $fixture.failure = $failure
        $fixture.pathWrites = 0
        $expected = switch ($failure) {
            'checksum' { '*Checksum verification failed*' }
            'download' { '*Fixture download failure*' }
            'api' { '*Could not determine the latest stable release*' }
            'metadata' { '*does not identify a stable version*' }
            'archive' { '*whether release v1.2.3 contains Windows*' }
            'missing-checksum' { '*no canonical checksum file*' }
            'malformed' { '*no unique valid entry*' }
            'duplicate' { '*no unique valid entry*' }
            'missing-binary' { '*does not contain dg.exe*' }
            'locked' { '*Close running dg processes*' }
            'path' { '*Fixture PATH persistence denied*' }
            'path-silent' { '*user PATH write did not persist*' }
            'path-partial' { '*Fixture PATH partial failure*' }
        }
        # Force a PATH update for the persistence failure, then verify rollback.
        if ($failure -like 'path*') {
            $null = Microsoft.PowerShell.Management\Set-ItemProperty -LiteralPath $registryPath -Name Path -Value $baseUserPath
            $beforeUserPath = $baseUserPath
        }
        $lock = $null
        if ($failure -eq 'locked') { $lock = [IO.File]::Open($destination, 'Open', 'Read', 'Read') }
        $message = ''
        try {
            if ($failure -in @('locked', 'path', 'path-silent', 'path-partial')) { & $installer -Version '1.2.4' }
            else { & $bootstrap }
        } catch { $message = $_.Exception.Message }
        finally { if ($null -ne $lock) { $lock.Dispose() } }
        Assert ($message -like $expected) "Wrong $failure failure: $message"
        Assert ((Get-FileHash -LiteralPath $destination).Hash -eq $beforeHash) "$failure replaced the existing binary"
        Assert ((Read-UserPath) -ceq $beforeUserPath -and $env:PATH -ceq $beforeProcessPath) "$failure modified PATH"
        Assert-Version
        Assert-CleanStaging
        Assert (@(Get-ChildItem -LiteralPath $installDir -Filter '.dg-*').Count -eq 0) "$failure leaked replacement files"
    }
    $fixture.failure = ''

    # Local help and invalid settings must never call a download function.
    $fixture.downloads = @()
    $env:DG_NON_INTERACTIVE = 'invalid'
    $help = & $installer -Help
    Assert (($help -join "`n") -like '*-Version*') 'Help omitted options'
    Assert ($fixture.downloads.Count -eq 0) 'Help downloaded a release'
    foreach ($argsToTry in @(@{ Version = '../bad' ; NonInteractive = $true }, @{ Version = '01.2.3'; NonInteractive = $true }, @{})) {
        $message = ''
        try { & $installer @argsToTry } catch { $message = $_.Exception.Message }
        Assert ($message -match 'Invalid version|DG_NON_INTERACTIVE must be 0 or 1') "Invalid option accepted: $message"
    }
    Assert ($fixture.downloads.Count -eq 0) 'Invalid options downloaded a release'

    # Execute the guide's help bootstrap; only the script itself is downloaded.
    $guide = Get-Content -LiteralPath (Join-Path $repo 'docs/WINDOWS_INSTALLATION.md')
    $helpLine = @($guide | Where-Object { $_.StartsWith('& ([scriptblock]') -and $_.EndsWith(' -Help') })
    Assert ($helpLine.Count -eq 1) 'Missing Windows installation guide help command'
    $help = & ([scriptblock]::Create($helpLine[0]))
    Assert (($help -join "`n") -like '*-Version*') 'Documented help omitted options'
    Assert ($fixture.downloads.Count -eq 1 -and $fixture.downloads[0] -eq 'https://alcubi.ai/delegator/install.ps1') 'Documented help downloaded a release'
    $fixture.downloads = @()

    # Run the documented options form exactly, overriding all three env defaults.
    $env:DG_VERSION = 'invalid'
    $env:DG_INSTALL_DIR = Join-Path $root 'wrong directory'
    $destination = Join-Path $env:LOCALAPPDATA 'Delegator Tools\dg.exe'
    $env:PATH = $baseProcessPath
    $optionsLine = @($guide | Where-Object { $_.StartsWith('& ([scriptblock]') -and -not $_.EndsWith(' -Help') })
    Assert ($optionsLine.Count -eq 1) 'Missing Windows installation guide options command'
    & ([scriptblock]::Create($optionsLine[0]))
    Assert-Version
    Assert (-not (Test-Path -LiteralPath $env:DG_INSTALL_DIR)) 'Environment overrode explicit directory'
    Assert (-not ($fixture.downloads -contains 'https://api.github.com/repos/alcubie/delegator/releases/latest')) 'Explicit version used latest API'
    $env:DG_NON_INTERACTIVE = '1'
    $env:DG_VERSION = 'v1.2.4'
    $env:DG_INSTALL_DIR = Split-Path $destination -Parent
    & $installer
    Assert-Version 'dg v1.2.4'
    # An explicit version overrides a valid environment version too.
    & $installer -Version '1.2.3' -NonInteractive
    Assert-Version
    & $installer -Version 'v1.2.3-rc.1+build.7'
    Assert ($fixture.downloads -contains 'https://github.com/alcubie/delegator/releases/download/v1.2.3-rc.1%2Bbuild.7/delegator_1.2.3-rc.1+build.7_windows_amd64.zip') 'Explicit prerelease/build version was not selected'
    $fixture.legacy = $true
    & $installer -Version 'v1.2.3'
    Assert-Version
    Assert ($fixture.downloads -contains 'https://github.com/alcubie/delegator/releases/download/v1.2.3/alcubi-delegator_1.2.3_windows_amd64.zip') 'Historical prefix was not used'
    $fixture.legacy = $false
    Assert-CleanStaging

    # An existing file cannot be used as a directory (even under elevated CI).
    $blocked = Join-Path $root 'blocked destination'
    Set-Content -LiteralPath $blocked -Value 'keep'
    $message = ''
    try { & $installer -InstallDir $blocked } catch { $message = $_.Exception.Message }
    Assert ($message -like '*check directory and user PATH permissions*') "Wrong destination error: $message"
    Assert ((Get-Content -LiteralPath $blocked) -eq 'keep') 'Unwritable destination changed'
    Assert-Version
    Assert-CleanStaging
    $env:DG_VERSION = '1.2.3'
    # Real Windows ACL denial and failed first-install PATH persistence.
    $denied = Join-Path $root 'denied directory'
    $null = New-Item -ItemType Directory -Path $denied
    $acl = Get-Acl -LiteralPath $denied
    $deniedAcl = Get-Acl -LiteralPath $denied
    $sid = [Security.Principal.WindowsIdentity]::GetCurrent().User
    $deny = New-Object Security.AccessControl.FileSystemAccessRule($sid, 'Write', 'Deny')
    $deniedAcl.AddAccessRule($deny)
    Set-Acl -LiteralPath $denied -AclObject $deniedAcl
    try {
        $message = ''
        try { & $installer -InstallDir $denied } catch { $message = $_.Exception.Message }
        Assert ($message -like '*check directory and user PATH permissions*') "ACL denial was not reported: $message"
        Assert (-not (Test-Path -LiteralPath (Join-Path $denied 'dg.exe'))) 'ACL denial installed a binary'
        Assert-Version
    } finally { Set-Acl -LiteralPath $denied -AclObject $acl }
    $previousPath = Read-UserPath
    $freshDir = Join-Path $root 'fresh failure'
    Microsoft.PowerShell.Management\Remove-ItemProperty -LiteralPath $registryPath -Name Path
    $fixture.failure = 'path-partial'
    $fixture.pathWrites = 0
    try {
        $message = ''
        try { & $installer -InstallDir $freshDir } catch { $message = $_.Exception.Message }
        Assert ($message -like '*Fixture PATH partial failure*') "Fresh PATH failure was not reported: $message"
        Assert (-not (Test-Path -LiteralPath (Join-Path $freshDir 'dg.exe'))) 'Failed fresh install left dg.exe'
        $key = Microsoft.PowerShell.Management\Get-Item -LiteralPath $registryPath
        try { Assert (-not ($key.GetValueNames() -contains 'Path')) 'Failed fresh install left a PATH value' }
        finally { $key.Close() }
        Assert (@(Get-ChildItem -LiteralPath $freshDir -Filter '.dg-*').Count -eq 0) 'Failed fresh install leaked files'
        Assert-CleanStaging
    } finally {
        $fixture.failure = ''
        $null = Microsoft.PowerShell.Management\New-ItemProperty -LiteralPath $registryPath -Name Path -Value $previousPath -PropertyType ExpandString
    }

    # Start-Process creates a new console on Windows. Hide it while retaining
    # real console handles; the fixture EXE records any onboarding invocation.
    $shell = (Get-Process -Id $PID).Path
    foreach ($scenarioName in @('interactive', 'switch', 'environment', 'host', 'redirected')) {
        $quotedScript = $PSCommandPath.Replace("'", "''")
        $quotedInstaller = $installer.Replace("'", "''")
        $quotedRoot = $consoleRoot.Replace("'", "''")
        $command = "& '$quotedScript' -InstallerPath '$quotedInstaller' -Scenario '$scenarioName' -FixtureRoot '$quotedRoot'"
        $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
        $arguments = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-EncodedCommand', $encoded)
        if ($scenarioName -eq 'host') { $arguments = @('-NonInteractive') + $arguments }
        $start = @{ FilePath = $shell; ArgumentList = $arguments; PassThru = $true; WindowStyle = 'Hidden' }
        if ($scenarioName -eq 'redirected') {
            $inputFile = Join-Path $consoleRoot 'stdin.txt'
            Set-Content -LiteralPath $inputFile -Value ''
            $start.RedirectStandardInput = $inputFile
        }
        $child = Start-Process @start
        try {
            if (-not $child.WaitForExit(30000)) { $child.Kill(); throw "Installer hung in $scenarioName scenario" }
            $resultFile = Join-Path $consoleRoot "$scenarioName.result"
            Assert (Test-Path -LiteralPath $resultFile) "Child failed to report $scenarioName result"
            $result = Get-Content -LiteralPath $resultFile -Raw
            Assert ($result.Trim() -eq 'passed') "$scenarioName failed: $result"
        } finally { $child.Dispose() }
    }

    # ARM selection/extraction only: this x64 runner never executes the ARM EXE.
    $fixture.architecture = 12
    & $bootstrap
    Assert ($fixture.downloads -contains 'https://github.com/alcubie/delegator/releases/download/v1.2.3/delegator_1.2.3_windows_arm64.zip') 'ARM64 asset was not selected'
    Assert ((Get-FileHash -LiteralPath $destination).Hash -eq (Get-FileHash -LiteralPath (Join-Path $root '1.2.3-arm64\dg.exe')).Hash) 'ARM64 payload was not installed'
    Assert-CleanStaging
    $fixture.architecture = 0
    $message = ''
    try { & $bootstrap } catch { $message = $_.Exception.Message }
    Assert ($message -like '*Unsupported Windows architecture*') 'Unsupported architecture was not rejected'
    Write-Host "Installer smoke checks passed on PowerShell $($PSVersionTable.PSVersion). ARM64 selection checked; ARM64 execution not tested."
} finally {
    foreach ($name in $saved.Keys) { [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process') }
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
    if (Test-Path -LiteralPath $registryPath) { Remove-Item -LiteralPath $registryPath -Recurse -Force }
    Assert ((Read-RealUserPath) -ceq $originalUserPath) 'The real user PATH changed'
}
