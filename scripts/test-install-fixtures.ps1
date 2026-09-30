# Shared, isolated download and registry proxies for Windows installer checks.
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
    if ($Uri -eq 'https://alcubi.ai/delegator/install.ps1') {
        # Match the web response's string conversion used by iwr | iex.
        return [IO.File]::ReadAllText($installer)
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
function Read-RealUserPath {
    $key = Microsoft.PowerShell.Management\Get-Item -LiteralPath 'HKCU:\Environment'
    try { $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) }
    finally { $key.Close() }
}
function Assert-CleanStaging {
    foreach ($stage in $script:stages) { Assert (-not (Test-Path -LiteralPath $stage)) "Staging directory leaked: $stage" }
}
