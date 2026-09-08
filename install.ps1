param(
    [string]$Version = '@RELEASE_TAG@',
    [string]$Destination = ''
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$stage = $null
try {
    if ($env:OS -ne 'Windows_NT') { throw 'Use install.sh on macOS or Linux.' }
    $architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
    switch ($architecture) { 'x64' { $architecture = 'amd64' }; 'arm64' {}; default { throw 'Only amd64 and arm64 release builds are available.' } }
    if (-not $Destination) { $Destination = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'AgentWarmup' }
    if (-not [IO.Path]::IsPathRooted($Destination)) { throw 'Destination must be an absolute directory.' }
    $Destination = [IO.Path]::GetFullPath($Destination).TrimEnd('\')
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    if ($Version.StartsWith('@')) {
        $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/OliverGrabner/agentwarmup/releases/latest' -Headers @{ Accept = 'application/vnd.github+json' }
        $Version = $release.tag_name
    }
    if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$') { throw 'Invalid release tag; use vMAJOR.MINOR.PATCH or a versioned prerelease.' }
    $number = $Version.Substring(1)
    $archive = "agentwarmup_${number}_windows_${architecture}.zip"
    $base = "https://github.com/OliverGrabner/agentwarmup/releases/download/$Version"
    $stage = Join-Path ([IO.Path]::GetTempPath()) ('agentwarmup-install-' + [Guid]::NewGuid().ToString('N'))
    $null = New-Item -ItemType Directory -Path $stage
    $archivePath = Join-Path $stage $archive
    $manifest = Join-Path $stage 'checksums.txt'
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$archive" -OutFile $archivePath
    Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile $manifest
    $entries = @([IO.File]::ReadAllLines($manifest) | Where-Object { $_ -match ('^[a-fA-F0-9]{64}\s+' + [regex]::Escape($archive) + '$') })
    if ($entries.Count -ne 1) { throw 'Checksum manifest has no unique archive entry.' }
    $expected = ($entries[0] -split '\s+')[0]
    if ((Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash -ne $expected) { throw 'Checksum mismatch; the downloaded archive was not executed.' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($archivePath)
    try {
        if ($zip.Entries.Count -ne 2 -or @($zip.Entries | Where-Object { $_.FullName -notin @('agentwarmup.exe', 'LICENSE') }).Count -ne 0) { throw 'Archive contains unexpected files.' }
        foreach ($entry in $zip.Entries) {
            $output = Join-Path $stage $entry.FullName
            [IO.Compression.ZipFileExtensions]::ExtractToFile($entry, $output, $false)
        }
    } finally { $zip.Dispose() }
    $executable = Join-Path $stage 'agentwarmup.exe'
    $reported = & $executable --version
    if ($LASTEXITCODE -ne 0 -or $reported -ne "agentwarmup $number") { throw 'The executable cannot run here or reports an unexpected version.' }
    Write-Host "Installing AgentWarmup $number"
    & $executable install --home $Destination
    if ($LASTEXITCODE -ne 0) { throw 'Installation did not complete; see the explanation above.' }
    Write-Host 'Open a new terminal to use agentwarmup.'
} catch {
    Write-Error ("AgentWarmup: " + $_.Exception.Message) -ErrorAction Continue
    exit 1
} finally {
    if ($stage -and (Test-Path -LiteralPath $stage)) {
        $resolved = (Resolve-Path -LiteralPath $stage).ProviderPath
        $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
        if ($resolved.StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -and ([IO.Path]::GetFileName($resolved) -like 'agentwarmup-install-*')) {
            Remove-Item -LiteralPath $resolved -Recurse -Force
        }
    }
}
