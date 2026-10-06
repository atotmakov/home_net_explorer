#Requires -Version 5.1
# Deploy Home Net Explorer to the NAS (quickstart.md §3).
# Gets the image (local archive, else crane pull), streams it to the NAS over SSH, loads it and
# (re)starts the container with docker compose. Same flow as the cctv-ui project.
#
# Image source: a local archive (IMAGE_ARCHIVE) is used if it exists; only when it is absent is
# the image pulled from GitHub (ghcr.io) with crane. The archive is kept for the next deploy.
#   .\deploy.ps1          use the local archive if present, else pull
#   .\deploy.ps1 -Pull    always pull a fresh image from GitHub (refreshes the archive)
param([switch]$Pull)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ── Load deploy config ────────────────────────────────────────────────────────
if (-not (Test-Path '.env.deploy')) {
    Write-Error "ERROR: .env.deploy not found. Copy .env.deploy.example and fill it in."
    exit 1
}

$config = @{}
Get-Content '.env.deploy' | Where-Object { $_ -match '^\s*[^#=]' } | ForEach-Object {
    if ($_ -match '^([^=]+)=(.*)$') {
        $config[$Matches[1].Trim()] = $Matches[2].Trim() -replace '\s*#.*$', ''
    }
}

$NasHost  = if ($config['NAS_HOST'])       { $config['NAS_HOST'] }       else { throw "NAS_HOST must be set in .env.deploy" }
$NasUser  = if ($config['NAS_USER'])       { $config['NAS_USER'] }       else { throw "NAS_USER must be set in .env.deploy" }
$NasDir   = if ($config['NAS_DIR'])        { $config['NAS_DIR'] }        else { '/volume1/docker/home-net-explorer' }
$SshKey   = if ($config['SSH_KEY'])        { $config['SSH_KEY'] }        else { '' }
$Image    = if ($config['IMAGE'])          { $config['IMAGE'] }          else { 'ghcr.io/atotmakov/home_net_explorer:latest' }
$Platform = if ($config['IMAGE_PLATFORM']) { $config['IMAGE_PLATFORM'] } else { 'linux/amd64' }
$Port     = if ($config['HNE_PORT'])       { $config['HNE_PORT'] }       else { '8080' }
$Archive  = if ($config['IMAGE_ARCHIVE'])  { $config['IMAGE_ARCHIVE'] }  else { 'home-net-explorer.tar.gz' }
$RemoteArchive = 'home-net-explorer.tar.gz'

# SSH/SCP option arrays
$SshOpts = @('-o', 'StrictHostKeyChecking=no')
if ($SshKey) { $SshOpts += @('-i', $SshKey) }

$sshBin = "$env:SystemRoot\System32\OpenSSH\ssh.exe"

# Transfer a local file to remote via SSH stdin — no SFTP/SCP needed
function Send-FileViaSsh {
    param([string]$LocalPath, [string]$RemotePath)
    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $sshBin
    $startInfo.Arguments = ($SshOpts + @("${NasUser}@${NasHost}", "cat > '$RemotePath'")) -join ' '
    $startInfo.RedirectStandardInput = $true
    $startInfo.UseShellExecute = $false
    $proc = [System.Diagnostics.Process]::Start($startInfo)
    $fileStream = [System.IO.File]::OpenRead((Resolve-Path $LocalPath).Path)
    $fileStream.CopyTo($proc.StandardInput.BaseStream)
    $fileStream.Close()
    $proc.StandardInput.Close()
    $proc.WaitForExit()
    if ($proc.ExitCode -ne 0) { throw "SSH transfer failed for $LocalPath" }
}

# Run a multi-line bash script on the remote host via SSH stdin, writing raw UTF-8 bytes to the
# child's stdin stream (piping a PowerShell string through `|` reintroduces `\r\n`).
function Invoke-RemoteScript {
    param([string]$Script)
    $startInfo = New-Object System.Diagnostics.ProcessStartInfo
    $startInfo.FileName = $sshBin
    $startInfo.Arguments = ($SshOpts + @("${NasUser}@${NasHost}", "bash")) -join ' '
    $startInfo.RedirectStandardInput = $true
    $startInfo.UseShellExecute = $false
    $proc = [System.Diagnostics.Process]::Start($startInfo)
    $bytes = [System.Text.Encoding]::UTF8.GetBytes(($Script -replace "`r`n", "`n"))
    $proc.StandardInput.BaseStream.Write($bytes, 0, $bytes.Length)
    $proc.StandardInput.Close()
    $proc.WaitForExit()
    return $proc.ExitCode
}

# ── Step 1: Find the image: local archive first, GitHub only if absent ───────
$useLocal = (Test-Path $Archive) -and -not $Pull
if ($useLocal) {
    $info = Get-Item $Archive
    Write-Host ("==> Using local image archive {0} ({1:N0} MB, saved {2:yyyy-MM-dd HH:mm})" -f $info.FullName, ($info.Length / 1MB), $info.LastWriteTime)
    Write-Host "    (run .\deploy.ps1 -Pull to fetch the latest image from GitHub instead)"
} else {
    # ── Install crane if not present ──────────────────────────────────────────────
    $craneDir = "$env:LOCALAPPDATA\crane"
    $craneBin = "$craneDir\crane.exe"
    if (-not (Test-Path $craneBin)) {
        Write-Host "==> crane not found, installing..."
        New-Item -ItemType Directory -Force -Path $craneDir | Out-Null
        $tgz = "$env:TEMP\crane.tar.gz"
        curl.exe -fsSL -o $tgz "https://github.com/google/go-containerregistry/releases/latest/download/go-containerregistry_Windows_x86_64.tar.gz"
        if ($LASTEXITCODE -ne 0) { throw "Failed to download crane" }
        & "$env:SystemRoot\System32\tar.exe" -xzf $tgz -C $craneDir crane.exe
        Remove-Item $tgz
        Write-Host "==> crane installed at $craneBin"
    }
    $env:PATH = "$craneDir;$env:PATH"

    # The image is multi-arch; pick the NAS's architecture (IMAGE_PLATFORM). Pull to a temp file
    # so a failed download never replaces a good archive.
    if ($Pull) { Write-Host "==> -Pull: fetching a fresh image from GitHub" } else { Write-Host "==> No local archive at $Archive" }
    Write-Host "==> Pulling Docker image $Image ($Platform)..."
    $tmpArchive = "$Archive.partial"
    & $craneBin pull --platform=$Platform --format=tarball $Image $tmpArchive
    if ($LASTEXITCODE -ne 0) { Remove-Item -Force -ErrorAction SilentlyContinue $tmpArchive; throw "crane pull failed" }
    Move-Item -Force $tmpArchive $Archive
    Write-Host "==> Saved image archive to $Archive"
}

# ── Step 2: Upload archive + compose file ────────────────────────────────────
Write-Host "==> Uploading to ${NasUser}@${NasHost}:${NasDir}/ ..."
& $sshBin @SshOpts "${NasUser}@${NasHost}" "mkdir -p '$NasDir' && chown `$USER '$NasDir'"
if ($LASTEXITCODE -ne 0) { throw "Failed to create directory $NasDir on NAS" }
Write-Host "  -> Transferring archive..."
Send-FileViaSsh $Archive "${NasDir}/${RemoteArchive}"
Write-Host "  -> Transferring docker-compose.yml..."
Send-FileViaSsh deploy/compose.yaml "${NasDir}/docker-compose.yml"

# ── Step 3: Load image and restart container on NAS ──────────────────────────
Write-Host "==> Loading image and starting container on NAS..."
$remoteScript = @"
set -euo pipefail
export PATH="`$PATH:/usr/local/bin:/usr/bin"
cd '$NasDir'
echo '  -> Loading image...'
sudo docker load < '$RemoteArchive'
rm -f '$RemoteArchive'
# The container runs as this SSH user (HNE_UID/HNE_GID), so the data folder needs no sudo:
# only docker itself runs with sudo, which DSM allows without a password.
echo '  -> Preparing data folder...'
mkdir -p data
if [ ! -w data ]; then
    echo "  !! '$NasDir/data' is not writable by `$(id -un). Fix once on the NAS: sudo chown -R `$(id -u):`$(id -g) '$NasDir/data'" >&2
    exit 1
fi
printf 'HNE_PORT=%s\nHNE_UID=%s\nHNE_GID=%s\n' '$Port' "`$(id -u)" "`$(id -g)" > .env
echo '  -> Starting container...'
sudo docker compose up -d --remove-orphans
sudo docker compose ps
"@
$exitCode = Invoke-RemoteScript -Script $remoteScript
if ($exitCode -ne 0) { throw "Remote deploy failed" }

# ── Step 4: Health check from this PC ────────────────────────────────────────
Write-Host "==> Waiting for http://${NasHost}:${Port}/healthz ..."
$healthy = $false
for ($i = 0; $i -lt 30; $i++) {
    $body = curl.exe -fsS --max-time 3 "http://${NasHost}:${Port}/healthz" 2>$null
    if ($LASTEXITCODE -eq 0 -and $body -eq 'ok') { $healthy = $true; break }
    Start-Sleep -Seconds 2
}
if (-not $healthy) {
    throw "Server did not become healthy. Check: ssh ${NasUser}@${NasHost} 'cd $NasDir && sudo docker compose logs'"
}

Write-Host ""
Write-Host "Done! Open http://${NasHost}:${Port} (first visit asks you to set the owner password)."
