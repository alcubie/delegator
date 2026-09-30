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
    $script:pathWrites++
    if ($script:failure -eq 'path' -and $script:pathWrites -eq 1) { throw 'Fixture PATH persistence denied' }
    if ($script:failure -eq 'path-silent' -and $script:pathWrites -eq 1) { return }
    Microsoft.PowerShell.Management\New-ItemProperty -LiteralPath $registryPath -Name $Name -Value $Value -PropertyType $PropertyType -Force
    if ($script:failure -eq 'path-partial' -and $script:pathWrites -eq 1) { throw 'Fixture PATH partial failure' }
}
function Remove-ItemProperty {
    param([string] $LiteralPath, [string] $Name)
    Assert ($LiteralPath -eq 'HKCU:\Environment' -and $Name -eq 'Path') 'Unexpected registry removal'
    Microsoft.PowerShell.Management\Remove-ItemProperty -LiteralPath $registryPath -Name $Name
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
        # Support both the response Content property and iwr | iex conversion.
        $response = [pscustomobject]@{ Content = [IO.File]::ReadAllText($installer) }
        $response | Add-Member -MemberType ScriptMethod -Name ToString -Value { $this.Content } -Force
        return $response
    }
    if ($Uri -eq 'https://api.github.com/repos/alcubie/delegator/releases/latest') {
        if ($script:failure -eq 'api') { throw 'Fixture API failure' }
        if ($script:failure -eq 'metadata') { return [pscustomobject]@{ Content = '{"tag_name":"v1.2.3-rc.1","draft":false,"prerelease":true}' } }
        return [pscustomobject]@{ Content = '{"tag_name":"v1.2.3","draft":false,"prerelease":false}' }
    }
    Assert ($Uri -match '^https://github.com/alcubie/delegator/releases/download/v1\.2\.[34](-rc\.1%2Bbuild\.7)?/') "Unexpected release URL: $Uri"
    $script:stages += Split-Path $OutFile -Parent
    if ($script:failure -eq 'download') { throw 'Fixture download failure' }
    $name = ($Uri -split '/')[-1]
    if ($script:legacy -and $name.StartsWith('delegator_')) { throw 'Fixture modern filename not found' }
    if ($name.EndsWith('_checksums.txt')) {
        switch ($script:failure) {
            'missing-checksum' { throw 'Fixture checksum file not found' }
            'checksum' { Set-Content -LiteralPath $OutFile -Value (('0' * 64) + '  delegator_1.2.3_windows_amd64.zip'); return }
            'malformed' { Set-Content -LiteralPath $OutFile -Value 'invalid  delegator_1.2.3_windows_amd64.zip'; return }
            'duplicate' {
                $lines = Get-Content -LiteralPath (Join-Path $root $name)
                Set-Content -LiteralPath $OutFile -Value ($lines + $lines)
                return
            }
            'missing-binary' {
                $hash = (Get-FileHash -LiteralPath (Join-Path $root 'empty.zip')).Hash
                Set-Content -LiteralPath $OutFile -Value "$hash  delegator_1.2.3_windows_amd64.zip"
                return
            }
        }
    } elseif ($script:failure -eq 'missing-binary') {
        Copy-Item -LiteralPath (Join-Path $root 'empty.zip') -Destination $OutFile
        return
    } elseif ($script:failure -eq 'archive') { throw 'Fixture archive unavailable' }
    Copy-Item -LiteralPath (Join-Path $root $name) -Destination $OutFile
}
function Read-UserPath {
    $key = Microsoft.PowerShell.Management\Get-Item -LiteralPath $registryPath
    try { $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) }
    finally { $key.Close() }
}
function Assert-CleanStaging {
    foreach ($stage in $script:stages) { Assert (-not (Test-Path -LiteralPath $stage)) "Staging directory leaked: $stage" }
}
function Read-RealUserPath {
    $key = Microsoft.PowerShell.Management\Get-Item -LiteralPath 'HKCU:\Environment'
    try { $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) }
    finally { $key.Close() }
}
