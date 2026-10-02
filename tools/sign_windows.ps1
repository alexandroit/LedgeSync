<#
.SYNOPSIS
  Authenticode-sign LedgeSync Windows executables and installers with an RFC 3161
  timestamp, then verify each signature. Nothing secret is passed on the command line.

.DESCRIPTION
  Signing mode is selected by LEDGESYNC_WINDOWS_SIGNING:
    artifact-signing  Azure Artifact Signing (formerly Trusted Signing). Requires the
                      Microsoft signing dlib (LEDGESYNC_SIGNING_DLIB) and a metadata JSON
                      (LEDGESYNC_SIGNING_METADATA) naming Endpoint, CodeSigningAccountName and
                      CertificateProfileName. Azure credentials come from the standard
                      environment (AZURE_TENANT_ID/AZURE_CLIENT_ID with OIDC or a secret).
    thumbprint        A code-signing certificate whose key is in a hardware token or cloud
                      HSM exposed through the Windows certificate store.
                      LEDGESYNC_WINDOWS_CERT_SHA1 selects it.
  The timestamp server is LEDGESYNC_TIMESTAMP_URL (artifact signing default:
  http://timestamp.acs.microsoft.com). A self-signed certificate is rejected.

.EXAMPLE
  ./tools/sign_windows.ps1 -Path build/bin/LedgeSync.exe,build/installers/LedgeSync-setup.exe
#>
param(
  [Parameter(Mandatory = $true)][string[]]$Path,
  [switch]$VerifyOnly
)
$ErrorActionPreference = 'Stop'

function Find-SignTool {
  $kits = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
  # The Artifact Signing dlib ships for x64; Windows on ARM runs it under emulation.
  $arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -and $env:LEDGESYNC_WINDOWS_SIGNING -ne 'artifact-signing') { 'arm64' } else { 'x64' }
  $tool = Get-ChildItem -Path $kits -Filter signtool.exe -Recurse -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -like "*\$arch\signtool.exe" } | Sort-Object FullName -Descending | Select-Object -First 1
  if (-not $tool) { throw 'signtool.exe from the Windows SDK was not found.' }
  return $tool.FullName
}

function Test-Signature([string]$File, [string]$SignTool) {
  & $SignTool verify /pa /all /v $File | Out-Null
  if ($LASTEXITCODE -ne 0) { throw "signtool could not verify $File" }
  $signature = Get-AuthenticodeSignature -FilePath $File
  if ($signature.Status -ne 'Valid') { throw "Authenticode status for $File is $($signature.Status)" }
  if (-not $signature.TimeStamperCertificate) { throw "$File has no trusted timestamp" }
  if ($signature.SignerCertificate.Subject -eq $signature.SignerCertificate.Issuer) { throw "$File is self-signed" }
  [pscustomobject]@{
    file = Split-Path $File -Leaf
    status = "$($signature.Status)"
    signer = $signature.SignerCertificate.Subject
    thumbprint = $signature.SignerCertificate.Thumbprint
    timestamp = $signature.TimeStamperCertificate.Subject
    sha256 = (Get-FileHash -Algorithm SHA256 -Path $File).Hash.ToLowerInvariant()
  }
}

$signtool = Find-SignTool
$files = $Path | ForEach-Object { (Resolve-Path $_).Path }
if (-not $VerifyOnly) {
  $mode = $env:LEDGESYNC_WINDOWS_SIGNING
  switch ($mode) {
    'artifact-signing' {
      $timestamp = if ($env:LEDGESYNC_TIMESTAMP_URL) { $env:LEDGESYNC_TIMESTAMP_URL } else { 'http://timestamp.acs.microsoft.com' }
      if (-not (Test-Path $env:LEDGESYNC_SIGNING_DLIB) -or -not (Test-Path $env:LEDGESYNC_SIGNING_METADATA)) {
        throw 'Artifact signing needs LEDGESYNC_SIGNING_DLIB and LEDGESYNC_SIGNING_METADATA.'
      }
      & $signtool sign /v /fd SHA256 /tr $timestamp /td SHA256 /dlib $env:LEDGESYNC_SIGNING_DLIB /dmdf $env:LEDGESYNC_SIGNING_METADATA @files
    }
    'thumbprint' {
      $timestamp = if ($env:LEDGESYNC_TIMESTAMP_URL) { $env:LEDGESYNC_TIMESTAMP_URL } else { throw 'Set LEDGESYNC_TIMESTAMP_URL to the certificate authority timestamp server.' }
      if ($env:LEDGESYNC_WINDOWS_CERT_SHA1 -notmatch '^[0-9A-Fa-f]{40}$') { throw 'Set LEDGESYNC_WINDOWS_CERT_SHA1 to the certificate thumbprint.' }
      & $signtool sign /v /fd SHA256 /tr $timestamp /td SHA256 /sha1 $env:LEDGESYNC_WINDOWS_CERT_SHA1 @files
    }
    default { throw 'Set LEDGESYNC_WINDOWS_SIGNING to artifact-signing or thumbprint. Unsigned output is not a release.' }
  }
  if ($LASTEXITCODE -ne 0) { throw 'signtool failed to sign the files.' }
}
$results = foreach ($file in $files) { Test-Signature -File $file -SignTool $signtool }
$results | ConvertTo-Json -Depth 3
