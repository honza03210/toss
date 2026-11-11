# Installs Toss for Windows into %LOCALAPPDATA%\Programs\Toss and starts it at login.
# No administrator rights needed (except, optionally, for the firewall rule).
#   irm https://github.com/OWNER/toss/releases/latest/download/install.ps1 | iex
$ErrorActionPreference = 'Stop'
$repo = if ($env:TOSS_REPO) { $env:TOSS_REPO } else { 'OWNER/toss' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "toss-windows-$arch.zip"
$base = if ($env:TOSS_BASE_URL) { $env:TOSS_BASE_URL } else { "https://github.com/$repo/releases/latest/download" }
$dir = Join-Path $env:LOCALAPPDATA 'Programs\Toss'
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("toss-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

try {
    Write-Host "Downloading $asset..."
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile (Join-Path $tmp $asset)
    Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($asset))$" }
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
    if (-not $expected -or $expected -ne $actual) { throw "Checksum mismatch: not installing." }

    # Stop a running copy so the files can be replaced.
    if (Test-Path (Join-Path $dir 'toss.exe')) { & (Join-Path $dir 'toss.exe') uninstall | Out-Null }
    Get-Process tossw -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Expand-Archive -Force (Join-Path $tmp $asset) $dir
    Write-Host "Installed to $dir"

    # Put toss.exe on the user's PATH.
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not ($userPath -split ';' | Where-Object { $_ -eq $dir })) {
        [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ";$dir").TrimStart(';'), 'User')
        $env:Path += ";$dir"
        Write-Host "Added $dir to your PATH (new terminals pick it up)."
    }

    & (Join-Path $dir 'toss.exe') install

    # The phone connects to this PC, so Windows Defender Firewall must allow incoming connections.
    # TOSS_FIREWALL=skip (or yes) answers without asking, e.g. for unattended installs.
    $answer = if ($env:TOSS_FIREWALL -eq 'skip') { 'n' } elseif ($env:TOSS_FIREWALL) { 'y' } else {
        Read-Host "Allow Toss through Windows Firewall on private networks? This asks for administrator rights once. [Y/n]"
    }
    if ($answer -eq '' -or $answer -match '^[Yy]') {
        $exe = Join-Path $dir 'tossw.exe'
        $cmd = "netsh advfirewall firewall delete rule name=Toss >nul 2>&1 & netsh advfirewall firewall add rule name=Toss dir=in action=allow program=`"$exe`" enable=yes profile=private,domain"
        try {
            Start-Process cmd.exe -ArgumentList "/c $cmd" -Verb RunAs -Wait -WindowStyle Hidden
            Write-Host "Firewall rule added."
        } catch {
            Write-Host "Skipped the firewall rule. If the phone can't connect, allow tossw.exe in Windows Security > Firewall."
        }
    }
    Write-Host "Open a new terminal for the 'toss' command."
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
