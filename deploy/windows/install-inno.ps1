[CmdletBinding()]
param([Parameter(Mandatory = $true)][string] $Destination)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $IsWindows) { throw 'The pinned Inno Setup compiler requires Windows.' }
$Destination = [System.IO.Path]::GetFullPath($Destination)
if (Test-Path -LiteralPath $Destination) { throw "Refusing to replace compiler directory: $Destination" }
$download = Join-Path ([System.IO.Path]::GetTempPath()) ('ledgesync-inno-' + [guid]::NewGuid() + '.exe')
try {
    Invoke-WebRequest -Uri 'https://github.com/jrsoftware/issrc/releases/download/is-7_1_0/innosetup-7.1.0-x64.exe' -OutFile $download
    $expected = '0362a383ed217d4c4239b5933866dd96d3eb2102737da92f80f6057a4b40df2f'
    if ((Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash.ToLowerInvariant() -ne $expected) { throw 'Pinned Inno Setup download checksum mismatch.' }
    $signature = Get-AuthenticodeSignature -FilePath $download
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notmatch '(^|,\s*)O=Pyrsys B\.V\.(,|$)') {
        throw 'Pinned Inno Setup download does not have the expected valid Pyrsys B.V. Authenticode signature.'
    }
    $arguments = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', '/CURRENTUSER', ('/DIR="' + $Destination + '"'), '/NOICONS')
    $process = Start-Process -FilePath $download -ArgumentList $arguments -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "Inno Setup installation failed: $($process.ExitCode)" }
    $compiler = Join-Path $Destination 'ISCC.exe'
    if (-not (Test-Path -LiteralPath $compiler -PathType Leaf)) { throw 'ISCC.exe was not installed.' }
    Write-Output $compiler
} finally {
    if (Test-Path -LiteralPath $download) { Remove-Item -LiteralPath $download -Force }
}
