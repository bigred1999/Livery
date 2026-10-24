# Downloads a file for the Livery installer and checks its SHA-256.
# Exit codes: 0 ok, 2 download failed, 3 checksum mismatch.
param([string]$Url, [string]$Out, [string]$Sha256)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Out -UserAgent 'Livery-Installer'
} catch {
    Write-Output "Download failed: $($_.Exception.Message)"
    exit 2
}
$hash = (Get-FileHash -Algorithm SHA256 -Path $Out).Hash
if ($hash -ne $Sha256.ToUpper()) {
    Write-Output "Checksum mismatch: got $hash"
    Remove-Item $Out -Force -ErrorAction SilentlyContinue
    exit 3
}
Write-Output "Verified $Out"
exit 0
