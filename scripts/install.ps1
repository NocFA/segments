$ErrorActionPreference = "Stop"

$Repo = "NocFA/segments"
$Gitea = "https://git.nocfa.net"
$InstallDir = if ($env:INSTALL_DIR) { $env:INSTALL_DIR } else { "$env:USERPROFILE\.local\bin" }

function Info($msg)  { Write-Host $msg -ForegroundColor Cyan }
function Ok($msg)    { Write-Host $msg -ForegroundColor Green }
function Err($msg)   { Write-Host $msg -ForegroundColor Red; exit 1 }

function Find-Go {
    $candidates = @(
        (Get-Command go -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source),
        "$env:ProgramFiles\Go\bin\go.exe",
        "$env:USERPROFILE\go\bin\go.exe"
    ) | Where-Object { $_ -and (Test-Path $_) }
    if ($candidates) { return $candidates[0] }
    return $null
}

function Find-GCC {
    $candidates = @(
        (Get-Command gcc -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source)
    ) | Where-Object { $_ -and (Test-Path $_) }
    if ($candidates) { return $candidates[0] }

    # Check common locations
    $dirs = @(
        "$env:LOCALAPPDATA\Microsoft\WinGet\Packages",
        "C:\msys64\mingw64\bin",
        "C:\mingw64\bin",
        "C:\TDM-GCC-64\bin"
    )
    foreach ($d in $dirs) {
        $gcc = Get-ChildItem -Path $d -Filter "gcc.exe" -Recurse -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($gcc) { return $gcc.FullName }
    }
    return $null
}

function Install-Go {
    Info "Installing Go via winget..."
    winget install GoLang.Go --accept-source-agreements --accept-package-agreements --silent
    if ($LASTEXITCODE -ne 0) { Err "Failed to install Go" }

    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")

    $go = Find-Go
    if (-not $go) { Err "Go installed but not found in PATH. Restart your terminal and re-run." }
    return $go
}

function Install-GCC {
    Info "Installing MinGW-w64 (GCC) via winget..."
    winget install BrechtSanders.WinLibs.POSIX.UCRT --accept-source-agreements --accept-package-agreements --silent
    if ($LASTEXITCODE -ne 0) { Err "Failed to install GCC/MinGW-w64" }

    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [System.Environment]::GetEnvironmentVariable("Path", "User")

    $gcc = Find-GCC
    if (-not $gcc) { Err "GCC installed but not found in PATH. Restart your terminal and re-run." }
    return $gcc
}

function Install-FromRelease {
    try {
        $resp = Invoke-RestMethod -Uri "$Gitea/api/v1/repos/$Repo/releases/latest" -ErrorAction Stop
        $version = $resp.tag_name
        $asset = $resp.assets | Where-Object { $_.name -match "windows" -and $_.name -match "\.exe$" } | Select-Object -First 1
        if ($asset) {
            Info "Downloading segments $version..."
            $tmp = "$env:TEMP\segments.exe"
            Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $tmp -ErrorAction Stop
            New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
            Move-Item -Path $tmp -Destination "$InstallDir\segments.exe" -Force
            return $true
        }
        Info "No Windows binary in $version, building from source..."
    } catch {}
    return $false
}

function Find-RepoRoot {
    # Walk up from script location looking for go.mod with our module
    $dir = Split-Path -Parent $PSScriptRoot
    if (Test-Path "$dir\go.mod") {
        $mod = Get-Content "$dir\go.mod" -First 1
        if ($mod -match "segments") { return $dir }
    }
    return $null
}

function Install-FromSource {
    $go = Find-Go
    if (-not $go) { $go = Install-Go }
    Info "Using Go: $go"

    $gcc = Find-GCC
    if (-not $gcc) { $gcc = Install-GCC }
    $gccDir = Split-Path $gcc
    Info "Using GCC: $gcc"

    $env:Path = "$gccDir;$env:Path"

    $repoRoot = Find-RepoRoot
    $cleanup = $false

    if ($repoRoot) {
        Info "Building from local source ($repoRoot)..."
        Push-Location $repoRoot
    } else {
        Info "Building from source..."
        $tmp = Join-Path $env:TEMP "segments-build-$(Get-Random)"
        New-Item -ItemType Directory -Path $tmp -Force | Out-Null

        $ErrorActionPreference = "Continue"
        git clone --depth=1 "$Gitea/$Repo.git" "$tmp\segments" 2>&1 | Out-Null
        $ErrorActionPreference = "Stop"
        if ($LASTEXITCODE -ne 0) { Err "git clone failed" }

        Push-Location "$tmp\segments"
        $cleanup = $true
    }

    Copy-Item "web\index.html" "internal\server\index.html" -Force
    $env:CGO_ENABLED = "1"
    $ErrorActionPreference = "Continue"
    & $go build -o segments.exe ./cmd/segments/ 2>&1 | Out-Host
    $ErrorActionPreference = "Stop"
    if ($LASTEXITCODE -ne 0) { Pop-Location; Err "Build failed" }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -Path "segments.exe" -Destination "$InstallDir\segments.exe" -Force
    Remove-Item -Path "segments.exe" -Force -ErrorAction SilentlyContinue
    Pop-Location

    if ($cleanup) { Remove-Item -Recurse -Force $tmp }
}

function Add-ToPath {
    $userPath = [System.Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -split ";" | Where-Object { $_ -eq $InstallDir }) { return }

    Info "Adding $InstallDir to user PATH..."
    [System.Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    $env:Path = "$InstallDir;$env:Path"
}

# --- Main ---

Write-Host ""
Info "Installing Segments..."
Write-Host ""

if (-not (Install-FromRelease)) {
    Install-FromSource
}

# Create sg alias (copy, since Windows doesn't have symlinks without admin)
Copy-Item "$InstallDir\segments.exe" "$InstallDir\sg.exe" -Force

Add-ToPath

Write-Host ""
Ok "Segments installed."
Write-Host ""
Write-Host "  sg setup   -- configure integrations (run this first)" -ForegroundColor Green
Write-Host "  sg init    -- initialize a project in the current directory" -ForegroundColor Green
Write-Host "  sg start   -- start the server" -ForegroundColor Green
Write-Host ""
