$ErrorActionPreference = 'Stop'
$installer = [scriptblock]::Create((Get-Content -Raw ./install.ps1))
$env:LOCALAPPDATA = Join-Path $env:RUNNER_TEMP ('whwmpc installer ' + [Guid]::NewGuid().ToString('N'))
$target = Join-Path $env:LOCALAPPDATA 'Programs\whwmpc\whwmpc.exe'

try {
    & $installer -Version v0.3.6
    if ((& $target --version) -cne 'whwmpc v0.3.6' -or $LASTEXITCODE -ne 0) {
        throw 'The pinned Windows installation did not run.'
    }

    $latest = (Invoke-RestMethod -UseBasicParsing 'https://api.github.com/repos/christiandoxa/whwmpc/releases/latest').tag_name
    & $installer
    if ((& $target --version) -cne "whwmpc $latest" -or $LASTEXITCODE -ne 0) {
        throw "The Windows upgrade did not run version $latest."
    }
    $image = [IO.File]::ReadAllBytes($target)
    $machine = [BitConverter]::ToUInt16($image, [BitConverter]::ToInt32($image, 0x3c) + 4)
    $expected = if ($env:RUNNER_ARCH -eq 'ARM64') { 0xaa64 } else { 0x8664 }
    if ($machine -ne $expected) {
        throw "The installed executable does not match $env:RUNNER_ARCH."
    }

    $hash = (Get-FileHash -LiteralPath $target).Hash
    function curl.exe {
        $output = $args[([Array]::IndexOf($args, '--output') + 1)]
        $value = if ($args[-1].EndsWith('/SHA256SUMS')) {
            $arch = if ($env:RUNNER_ARCH -eq 'ARM64') { 'arm64' } else { 'amd64' }
            "$('0' * 64)  whwmpc_v0.3.6_windows_$arch.zip"
        } else {
            'invalid archive fixture'
        }
        Set-Content -LiteralPath $output -Encoding Ascii -Value $value
        $global:LASTEXITCODE = 0
    }
    try {
        & $installer -Version v0.3.6
        throw 'The installer accepted a bad checksum.'
    } catch {
        if ($_.Exception.Message -notlike 'SHA-256 gagal diverifikasi*') { throw }
    }
    $installDir = Split-Path -Parent $target
    if ((Get-FileHash -LiteralPath $target).Hash -cne $hash -or @(Get-ChildItem -LiteralPath $installDir -Force).Count -ne 1) {
        throw 'The rejected download changed the installation or left staged files.'
    }
    Write-Output "Windows $env:RUNNER_ARCH, PowerShell $($PSVersionTable.PSVersion): install, upgrade, and checksum failure passed."
} finally {
    if (Test-Path -LiteralPath $env:LOCALAPPDATA) {
        Remove-Item -LiteralPath $env:LOCALAPPDATA -Recurse -Force
    }
}
