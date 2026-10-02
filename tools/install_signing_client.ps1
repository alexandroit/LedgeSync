<#
.SYNOPSIS
  Install the pinned Microsoft Artifact Signing client (signtool dlib) for
  tools/sign_windows.ps1, verifying the package hash before extraction.
  Writes LEDGESYNC_SIGNING_DLIB and LEDGESYNC_SIGNING_METADATA to GITHUB_ENV when present.
.PARAMETER Endpoint / Account / Profile
  Non-secret Artifact Signing endpoint URI, signing account and certificate profile names.
#>
param(
  [Parameter(Mandatory = $true)][string]$Endpoint,
  [Parameter(Mandatory = $true)][string]$Account,
  [Parameter(Mandatory = $true)][string]$Profile,
  [string]$Destination = 'build/signing'
)
$ErrorActionPreference = 'Stop'
$version = '1.0.128'
$sha256 = '74bd7d27e6ce1051409c38d9b46bc8df0400ecd643d51ffbf2ac00869061e40b'
$url = "https://api.nuget.org/v3-flatcontainer/microsoft.artifactsigning.client/$version/microsoft.artifactsigning.client.$version.nupkg"
New-Item -ItemType Directory -Force -Path $Destination | Out-Null
$package = Join-Path $Destination "microsoft.artifactsigning.client.$version.nupkg"
Invoke-WebRequest -Uri $url -OutFile $package -UseBasicParsing
$actual = (Get-FileHash -Algorithm SHA256 -Path $package).Hash.ToLowerInvariant()
if ($actual -ne $sha256) { throw "Artifact Signing client hash mismatch: $actual" }
$extract = Join-Path $Destination 'client'
Expand-Archive -Path $package -DestinationPath $extract -Force
$dlib = (Resolve-Path (Join-Path $extract 'bin/x64/Azure.CodeSigning.Dlib.dll')).Path
if ($Endpoint -notmatch '^https://[a-z0-9.-]+\.codesigning\.azure\.net/?$') { throw 'Unexpected Artifact Signing endpoint.' }
$metadata = Join-Path $Destination 'metadata.json'
[ordered]@{ Endpoint = $Endpoint; CodeSigningAccountName = $Account; CertificateProfileName = $Profile } | ConvertTo-Json | Set-Content -Path $metadata -Encoding utf8
if ($env:GITHUB_ENV) {
  "LEDGESYNC_SIGNING_DLIB=$dlib" | Out-File -FilePath $env:GITHUB_ENV -Append -Encoding utf8
  "LEDGESYNC_SIGNING_METADATA=$((Resolve-Path $metadata).Path)" | Out-File -FilePath $env:GITHUB_ENV -Append -Encoding utf8
  "LEDGESYNC_WINDOWS_SIGNING=artifact-signing" | Out-File -FilePath $env:GITHUB_ENV -Append -Encoding utf8
}
Write-Output "Artifact Signing client $version installed and verified."
