#Requires -Version 5.1
[CmdletBinding()]
param([Parameter(Position = 0)][string]$Version)

$ErrorActionPreference = 'Stop'

function Invoke-ReleaseDownload {
    & curl.exe --disable --fail --silent --show-error --location --proto '=https' --proto-redir '=https' @args
    if ($LASTEXITCODE -ne 0) {
        throw "Unduhan gagal (curl.exe $LASTEXITCODE)."
    }
}

if ($env:OS -ne 'Windows_NT' -or -not [Environment]::Is64BitOperatingSystem) {
    throw 'Installer ini memerlukan Windows 64-bit.'
}
$processor = Get-CimInstance -ClassName Win32_Processor -Property Architecture | Select-Object -First 1
$arch = switch ($processor.Architecture) {
    9 { 'amd64' }
    12 { 'arm64' }
    default { throw "Arsitektur Windows tidak didukung ($($processor.Architecture))." }
}

$repo = 'https://github.com/christiandoxa/whwmpc'
if (-not $PSBoundParameters.ContainsKey('Version')) {
    $latest = Invoke-ReleaseDownload --output NUL --write-out '%{url_effective}' "$repo/releases/latest"
    $tagUrl = "$repo/releases/tag/"
    if (-not $latest -or -not $latest.StartsWith($tagUrl, [StringComparison]::Ordinal)) {
        throw 'Tag rilis stabil tidak ditemukan.'
    }
    $Version = $latest.Substring($tagUrl.Length)
}
if ($Version -cnotmatch '^v[0-9]+\.[0-9]+\.[0-9]+\z') {
    throw 'Tag rilis harus berbentuk vX.Y.Z.'
}

if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA) -or -not [IO.Path]::IsPathRooted($env:LOCALAPPDATA)) {
    throw 'Folder LOCALAPPDATA tidak tersedia.'
}
$binDir = Join-Path $env:LOCALAPPDATA 'Programs\whwmpc'
$target = Join-Path $binDir 'whwmpc.exe'
$stage = Join-Path $binDir ('.whwmpc-' + [Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($stage) | Out-Null

try {
    $archive = "whwmpc_${Version}_windows_${arch}.zip"
    $base = "$repo/releases/download/$Version"
    $archivePath = Join-Path $stage $archive
    $manifest = Join-Path $stage 'SHA256SUMS'
    Invoke-ReleaseDownload --output $archivePath "$base/$archive"
    Invoke-ReleaseDownload --output $manifest "$base/SHA256SUMS"

    $pattern = '^([a-fA-F0-9]{64})[ \t]+' + [regex]::Escape($archive) + '\z'
    $checksums = @(Select-String -LiteralPath $manifest -Pattern $pattern -CaseSensitive)
    if ($checksums.Count -ne 1 -or (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash -ine $checksums[0].Matches[0].Groups[1].Value) {
        throw "SHA-256 gagal diverifikasi untuk $archive."
    }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $stagedExe = Join-Path $stage 'whwmpc.exe'
    $zip = [IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        $entries = @($zip.Entries | Where-Object { $_.FullName -ceq 'whwmpc.exe' })
        if ($entries.Count -ne 1 -or $entries[0].Length -eq 0) {
            throw "whwmpc.exe tidak valid di $archive."
        }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0], $stagedExe)
    } finally {
        $zip.Dispose()
    }

    $existing = Get-Item -LiteralPath $target -Force -ErrorAction SilentlyContinue
    if ($existing) {
        if ($existing.Attributes -band ([IO.FileAttributes]::Directory -bor [IO.FileAttributes]::ReparsePoint)) {
            throw "Target harus berupa file biasa ($target)."
        }
        $backup = "$stage.previous.exe"
        try {
            [IO.File]::Replace($stagedExe, $target, $backup)
        } catch {
            if ([IO.File]::Exists($backup) -and -not [IO.File]::Exists($target)) {
                [IO.File]::Move($backup, $target)
            }
            throw
        }
        [IO.File]::Delete($backup)
    } else {
        [IO.File]::Move($stagedExe, $target)
    }
} finally {
    [IO.Directory]::Delete($stage, $true)
}

Write-Output "whwmpc $Version terpasang di $target"
