# Finds cloud-sync "conflicted copy" duplicates in the Shogun2 save folder
# and helps you pick which one to keep. Double-click "Recover.bat" the
# moment a desync is noticed, before playing further turns.

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$ConfigFile = Join-Path $RepoRoot "config.json"

function Pause-Close {
    Read-Host "Press Enter to close this window"
}

if (-not (Test-Path $ConfigFile)) {
    Write-Host "Setup hasn't been run yet. Double-click Setup.bat first."
    Pause-Close
    exit 1
}

$Config = Get-Content $ConfigFile -Raw | ConvertFrom-Json

function Expand-Home($p) {
    if ($p.StartsWith("~")) {
        return $p -replace "^~", $env:USERPROFILE
    }
    return $p
}

$CloudRoot = Expand-Home $Config.cloud_root
$SyncTarget = Join-Path $CloudRoot $Config.sync_subfolder

if (-not (Test-Path $SyncTarget)) {
    Write-Host "Sync folder not found at $SyncTarget. Double-click Setup.bat first."
    Pause-Close
    exit 1
}

# Dropbox: "name (User's conflicted copy 2026-08-07).ext"
# OneDrive: "name-User's conflicted copy YYYY-MM-DD.ext" or "name-PCNAME.ext"
$Conflicts = Get-ChildItem -Path $SyncTarget -File |
    Where-Object { $_.Name -match "conflict" -or $_.Name -match "conflicted copy" }

if (-not $Conflicts) {
    Write-Host "No problem files found -- looks clean."
    Write-Host "Most recent saves in the shared folder:"
    Get-ChildItem -Path $SyncTarget | Sort-Object LastWriteTime -Descending | Select-Object -First 10 | Format-Table Name, LastWriteTime
    Pause-Close
    exit 0
}

Write-Host "Found files that look like sync conflicts (both players saved at the same moment):"
$Conflicts | Sort-Object LastWriteTime -Descending | ForEach-Object {
    Write-Host ("  {0}  (saved: {1})" -f $_.FullName, $_.LastWriteTime)
}

Write-Host ""
Write-Host "What to do:"
Write-Host "  1. Open File Explorer and go to the folder listed above."
Write-Host "  2. Talk to the other player -- figure out together which save is the"
Write-Host "     right one to keep (usually whichever one matches what you both"
Write-Host "     last actually saw in-game)."
Write-Host "  3. Delete the other (wrong) file(s) listed above -- just send them"
Write-Host "     to the Recycle Bin like normal."
Write-Host "  4. Both players fully quit Shogun2, confirm only one save remains"
Write-Host "     for that turn, then relaunch and reload the campaign together."
Pause-Close
