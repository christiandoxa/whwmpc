param([Parameter(Mandatory)][string]$Checker)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if (-not $IsWindows -or $env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted') {
    throw 'NTFS smoke requires a GitHub-hosted Windows runner'
}

$binary = (Resolve-Path -LiteralPath $Checker).ProviderPath
$nonce = [guid]::NewGuid().ToString('N').Substring(0, 12)
$label = "WHWMPC-NTFS-$nonce"
$directory = New-Item -Path (Join-Path $env:RUNNER_TEMP "whwmpc-ntfs-$nonce") -ItemType Directory
$image = Join-Path $directory.FullName 'fixture.vhdx'
$script = Join-Path $directory.FullName 'create-vhd.txt'
Set-Content -LiteralPath $script -Encoding Ascii -Value "create vdisk file=`"$image`" maximum=256 type=expandable"
& (Join-Path $env:SystemRoot 'System32\diskpart.exe') /s $script
if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $image -PathType Leaf)) {
    throw 'DiskPart could not create the temporary VHDX'
}

function Get-FixtureDisk {
    $disks = @(Get-DiskImage -ImagePath $image | Get-Disk)
    if ($disks.Count -ne 1 -or $disks[0].IsBoot -or $disks[0].IsSystem -or $disks[0].Size -ne 256MB) {
        throw 'The image does not map to one non-system 256 MiB disk'
    }
    return $disks[0]
}

$attached = $false
try {
    Mount-DiskImage -ImagePath $image -Access ReadWrite -NoDriveLetter | Out-Null
    $attached = $true
    $disk = Get-FixtureDisk
    if ($disk.PartitionStyle -ne 'RAW' -or $disk.IsOffline -or $disk.IsReadOnly) {
        throw 'Expected a new, writable VHDX for fixture setup'
    }

    $partitions = @(Initialize-Disk -InputObject $disk -PartitionStyle GPT -PassThru |
        New-Partition -UseMaximumSize -AssignDriveLetter)
    if ($partitions.Count -ne 1 -or $partitions[0].DiskNumber -ne $disk.Number -or -not $partitions[0].Guid) {
        throw 'The fixture partition is ambiguous'
    }
    $partitionGuid = $partitions[0].Guid
    Format-Volume -Partition $partitions[0] -FileSystem NTFS -NewFileSystemLabel $label -Confirm:$false | Out-Null

    Dismount-DiskImage -ImagePath $image | Out-Null
    $attached = $false
    $before = (Get-FileHash -LiteralPath $image -Algorithm SHA256).Hash

    Mount-DiskImage -ImagePath $image -Access ReadOnly | Out-Null
    $attached = $true
    $disk = Get-FixtureDisk
    if (-not $disk.IsReadOnly) {
        throw 'The checker fixture VHDX did not attach read-only'
    }
    $partitions = @($disk | Get-Partition | Where-Object Guid -eq $partitionGuid)
    if ($partitions.Count -ne 1) {
        throw 'The NTFS fixture partition identity changed'
    }
    $volume = Get-Volume -Partition $partitions[0]
    if ($volume.FileSystem -ne 'NTFS' -or $volume.FileSystemLabel -ne $label -or $volume.DriveLetter -notmatch '^[A-Z]$') {
        throw 'The remounted fixture does not have the expected NTFS label and drive letter'
    }
    $target = "$($volume.DriveLetter):"
    if ($target -eq $env:SystemDrive -or $volume.Path -notmatch '^\\\\\?\\Volume\{([0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12})\}\\$') {
        throw 'The NTFS fixture does not have a separate volume GUID path'
    }
    $volumeGuid = $Matches[1]
    $inventory = & $binary filesystem --json | ConvertFrom-Json
    $found = @($inventory.volumes | Where-Object { $_.name -eq $target -and $_.fstype -eq 'ntfs' -and $_.uuid -eq $volumeGuid })
    if ($LASTEXITCODE -ne 0 -or $found.Count -ne 1) {
        throw 'The checker could not match the NTFS fixture volume identity'
    }

    $result = & $binary filesystem --check $target --json | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0 -or $result.device -ne $target -or $result.availability -ne 'partial' -or
        $result.state -ne 'no_errors_observed' -or $result.exit_code -ne 0) {
        throw "Unexpected NTFS fixture check result: $($result | ConvertTo-Json -Compress)"
    }
} finally {
    if ($attached) {
        Dismount-DiskImage -ImagePath $image | Out-Null
    }
}

if ((Get-FileHash -LiteralPath $image -Algorithm SHA256).Hash -ne $before) {
    throw 'NTFS fixture image changed during the read-only check'
}
Write-Host "NTFS fixture $label check finished with state no_errors_observed and unchanged image checksum"
