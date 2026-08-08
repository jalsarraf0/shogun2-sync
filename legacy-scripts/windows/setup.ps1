# Junctions the Shogun2 multiplayer save folder into a cloud-synced
# folder so a save shared with another player propagates automatically.
# Double-click "Setup.bat" to run this -- no PowerShell/CLI knowledge
# needed. It will ask plain-language questions on first run.

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$ConfigFile = Join-Path $RepoRoot "config.json"

function Expand-Home($p) {
    if ($p.StartsWith("~")) {
        return $p -replace "^~", $env:USERPROFILE
    }
    return $p
}

function Pause-Close {
    Read-Host "Press Enter to close this window"
}

# ---- First run: ask plain questions instead of requiring a hand-edited file ----
if (-not (Test-Path $ConfigFile)) {
    Write-Host "=== Shogun 2 Save Sync - First Time Setup ==="
    Write-Host ""
    Write-Host "Which cloud service is the shared save folder in?"
    Write-Host "  1) Dropbox"
    Write-Host "  2) OneDrive"
    Write-Host "  3) Google Drive (must be set to 'Mirror' mode, not 'Stream' - see README)"
    $Choice = Read-Host "Enter 1, 2, or 3"

    switch ($Choice) {
        "1" {
            $Provider = "dropbox"
            $DefaultRoot = Join-Path $env:USERPROFILE "Dropbox"
        }
        "2" {
            $Provider = "onedrive"
            $DefaultRoot = if ($env:OneDrive) { $env:OneDrive } else { Join-Path $env:USERPROFILE "OneDrive" }
        }
        "3" {
            $Provider = "googledrive"
            $DefaultRoot = Join-Path $env:USERPROFILE "My Drive"
            Write-Host ""
            Write-Host "Reminder: Google Drive for Desktop must be set to 'Mirror files', not"
            Write-Host "'Stream files' (Settings -> Google Drive), or saves can silently fail to write."
        }
        default {
            Write-Host "Didn't understand that. Please run Setup.bat again and enter 1, 2, or 3."
            Pause-Close
            exit 1
        }
    }

    Write-Host ""
    Write-Host "The folder your friend shared with you should already be syncing to your computer."
    $CloudRootInput = Read-Host "Press Enter to use the usual location [$DefaultRoot], or type a different folder path"
    $CloudRoot = if ($CloudRootInput) { Expand-Home $CloudRootInput } else { $DefaultRoot }

    if (-not (Test-Path $CloudRoot)) {
        Write-Host ""
        Write-Host "Can't find '$CloudRoot'. Make sure $Provider is installed, has finished"
        Write-Host "its first sync, and that folder actually exists, then run Setup.bat again."
        Pause-Close
        exit 1
    }

    $SubfolderInput = Read-Host "Name of the shared save folder inside it [Shogun2SaveSync]"
    $SyncSubfolder = if ($SubfolderInput) { $SubfolderInput } else { "Shogun2SaveSync" }

    $Config = @{
        cloud_provider = $Provider
        cloud_root     = $CloudRoot
        sync_subfolder = $SyncSubfolder
    }
    $Config | ConvertTo-Json | Set-Content -Path $ConfigFile
    Write-Host "Saved settings (won't ask again next time)."
    Write-Host ""
}

$Config = Get-Content $ConfigFile -Raw | ConvertFrom-Json
$CloudRoot = Expand-Home $Config.cloud_root
$SyncTarget = Join-Path $CloudRoot $Config.sync_subfolder
$SavePath = Join-Path $env:APPDATA "The Creative Assembly\Shogun2\save_games_multiplayer"

if (-not (Test-Path $CloudRoot)) {
    Write-Host "Cloud folder '$CloudRoot' doesn't exist. Make sure the cloud app is installed"
    Write-Host "and has finished its first sync, then run Setup.bat again."
    Pause-Close
    exit 1
}

Write-Host "Save folder: $SavePath"
Write-Host "Sync target: $SyncTarget"

$Existing = Get-Item $SavePath -ErrorAction SilentlyContinue
if ($Existing -and $Existing.LinkType) {
    $CurrentTarget = (Get-Item $SavePath).Target
    if ($CurrentTarget -eq $SyncTarget) {
        Write-Host ""
        Write-Host "All set up already -- nothing to do. You're good to play."
        Pause-Close
        exit 0
    } else {
        Write-Host "Save folder already points somewhere else ($CurrentTarget)."
        Write-Host "Ask for help before continuing -- something's already set up differently."
        Pause-Close
        exit 1
    }
}

New-Item -ItemType Directory -Force -Path $SyncTarget | Out-Null

if (Test-Path $SavePath) {
    Write-Host "Moving your existing saves into the synced folder..."
    Get-ChildItem -Path $SavePath -Force | Move-Item -Destination $SyncTarget
    Remove-Item $SavePath -Force -Recurse
}

try {
    New-Item -ItemType Junction -Path $SavePath -Target $SyncTarget | Out-Null
} catch {
    Write-Host "Junction failed, trying a symbolic link instead (needs admin or Developer Mode)..."
    New-Item -ItemType SymbolicLink -Path $SavePath -Target $SyncTarget | Out-Null
}

Write-Host ""
Write-Host "All done! Your saves will now sync automatically. You're good to play."
Pause-Close
