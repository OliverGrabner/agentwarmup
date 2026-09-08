param([Parameter(Mandatory=$true)][string]$Tag, [string]$Output = 'dist', [switch]$CheckOnly)
$ErrorActionPreference = 'Stop'
if ($Tag -notmatch '^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?$') { throw 'Release tag must be a semantic version beginning with v.' }
$metadata = Get-Content -LiteralPath '.github/release.json' -Raw | ConvertFrom-Json
$prerelease = $Tag.Contains('-')
$previewFlag = $prerelease.ToString().ToLowerInvariant()
$includedProviders = @($metadata.providers.PSObject.Properties | ForEach-Object { $_.Name } | Sort-Object)
if (-not $prerelease) {
    $validated = @($metadata.providers.PSObject.Properties | Where-Object { $_.Value.validation -eq 'passed' -and $_.Value.minimumCLIVersion -and $_.Value.validatedModel })
    if (-not $metadata.releaseReady -or $validated.Count -eq 0 -or @($metadata.targets | Where-Object { -not $_.lifecycleValidated }).Count -gt 0) {
        throw 'Stable release blocked: provider reset evidence, platform lifecycle acceptance, and release readiness are pending. Build a versioned prerelease candidate.'
    }
    $includedProviders = @($validated | ForEach-Object { $_.Name } | Sort-Object)
}
if ($CheckOnly) { Write-Output 'Release gate passed.'; return }
$version = $Tag.Substring(1)
$destination = [IO.Path]::GetFullPath($Output)
if (Test-Path -LiteralPath $destination) { throw 'Output directory already exists; use a fresh destination to avoid mixing release assets.' }
$null = New-Item -ItemType Directory -Path $destination
$ownedStaging = @()
$originalGOOS=$env:GOOS; $originalGOARCH=$env:GOARCH; $originalCGO=$env:CGO_ENABLED
try {
    foreach ($target in $metadata.targets) {
        $env:GOOS=$target.os; $env:GOARCH=$target.arch; $env:CGO_ENABLED='0'
        $work=Join-Path $destination ('.work-' + $target.os + '-' + $target.arch)
        $null=New-Item -ItemType Directory -Path $work
        $ownedStaging += $work
        $name='agentwarmup'; if ($target.os -eq 'windows') { $name += '.exe' }
        $binary=Join-Path $work $name
        & go build -trimpath -ldflags "-s -w -X github.com/OliverGrabner/agentwarmup/internal/cli.Version=$version -X github.com/OliverGrabner/agentwarmup/internal/cli.Preview=$previewFlag -X github.com/OliverGrabner/agentwarmup/internal/provider.ReleaseProviders=$($includedProviders -join ',')" -o $binary .
        if ($LASTEXITCODE -ne 0) { throw "Build failed for $($target.os)/$($target.arch)." }
        Copy-Item -LiteralPath 'LICENSE' -Destination (Join-Path $work 'LICENSE')
        $asset="agentwarmup_${version}_$($target.os)_$($target.arch).$($target.extension)"
        $archive=Join-Path $destination $asset
        if ($target.extension -eq 'zip') {
            Compress-Archive -LiteralPath $binary,(Join-Path $work 'LICENSE') -DestinationPath $archive
        } else {
            & tar -czf $archive -C $work $name LICENSE
            if ($LASTEXITCODE -ne 0) { throw 'Archive creation failed.' }
        }
        $target | Add-Member -NotePropertyName asset -NotePropertyValue $asset
    }
    foreach ($installer in $metadata.installers) {
        $source=[IO.File]::ReadAllText((Join-Path (Get-Location) $installer))
        [IO.File]::WriteAllText((Join-Path $destination $installer),$source.Replace('@RELEASE_TAG@',$Tag),(New-Object Text.UTF8Encoding($false)))
    }
    $metadata.version=$version
    $metadata | Add-Member -NotePropertyName tag -NotePropertyValue $Tag
    $metadata | Add-Member -NotePropertyName includedProviders -NotePropertyValue $includedProviders
    $metadata | Add-Member -NotePropertyName npmPackage -NotePropertyValue "agentwarmup-${version}.tgz"
    $metadata | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath (Join-Path $destination 'release.json') -Encoding utf8
    & node '.github/scripts/package-npm.mjs' $destination
    if ($LASTEXITCODE -ne 0) { throw 'npm release packaging failed.' }
    $checksums=@(Get-ChildItem -LiteralPath $destination -File | Sort-Object Name | ForEach-Object { ((Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash.ToLowerInvariant()) + '  ' + $_.Name })
    [IO.File]::WriteAllText((Join-Path $destination 'checksums.txt'),($checksums -join "`n") + "`n",(New-Object Text.UTF8Encoding($false)))
} finally {
    $env:GOOS=$originalGOOS; $env:GOARCH=$originalGOARCH; $env:CGO_ENABLED=$originalCGO
    foreach ($work in $ownedStaging) {
        if (Test-Path -LiteralPath $work) {
            # Remove only staging trees created by this invocation, within its fresh output.
            $resolved=(Resolve-Path -LiteralPath $work).ProviderPath
            $resolvedDestination=(Resolve-Path -LiteralPath $destination).ProviderPath.TrimEnd([IO.Path]::DirectorySeparatorChar)
            if (-not $resolved.StartsWith($resolvedDestination + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -or (Get-Item -LiteralPath $work).Attributes.HasFlag([IO.FileAttributes]::ReparsePoint)) { throw 'Staging directory escaped release output.' }
            Remove-Item -LiteralPath $resolved -Recurse -Force
        }
    }
}
