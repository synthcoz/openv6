# openv6 one-line installer for Windows 10/11 (latest release from GitHub).
#   irm https://raw.githubusercontent.com/thephux/openv6-releases/main/install.ps1 | iex
# Downloads the installer, checks it against SHA256SUMS and installs it
# silently (Windows asks once for administrator permission).
& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue' # much faster downloads on Windows PowerShell 5.1
  [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

  $repo = if ($env:OPENV6_REPO) { $env:OPENV6_REPO } else { 'thephux/openv6-releases' }
  $ver = if ($env:OPENV6_VERSION) { $env:OPENV6_VERSION } else { 'latest' }
  $base = if ($ver -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$ver" }
  $name = 'openv6-setup-windows-amd64.exe'

  if (-not [Environment]::Is64BitOperatingSystem) { throw 'openv6 needs 64-bit Windows 10 or 11.' }

  $dir = Join-Path $env:TEMP ('openv6-' + [guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $dir | Out-Null
  $exe = Join-Path $dir $name
  try {
    Write-Host ">> downloading $name"
    Invoke-WebRequest -UseBasicParsing -Uri "$base/$name" -OutFile $exe

    $sums = (Invoke-WebRequest -UseBasicParsing -Uri "$base/SHA256SUMS").Content
    if ($sums -is [byte[]]) { $sums = [Text.Encoding]::UTF8.GetString($sums) }
    $want = $null
    foreach ($line in ($sums -split "`n")) {
      $f = $line.Trim() -split '\s+'
      if ($f.Count -ge 2 -and $f[1].TrimStart('*') -eq $name) { $want = $f[0].ToLower(); break }
    }
    if (-not $want) { throw "$name is not listed in SHA256SUMS, cannot verify it." }
    $got = (Get-FileHash -Algorithm SHA256 -Path $exe).Hash.ToLower()
    if ($got -ne $want) { throw "Checksum mismatch for $name. The download is damaged or was changed; nothing was installed." }
    Write-Host '>> checksum OK'

    Write-Host '>> installing (accept the Windows permission prompt)'
    $p = Start-Process -FilePath $exe -ArgumentList '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-' -Verb RunAs -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw "The installer exited with code $($p.ExitCode)." }

    Write-Host '>> done: openv6 runs in the background and starts with Windows.'
    Write-Host '   Dashboard: http://127.0.0.1:8080/  (also the openv6 icon next to the clock)'
    $app = Join-Path ${env:ProgramFiles} 'openv6\openv6.exe'
    if (Test-Path $app) { Start-Process -FilePath $app -ArgumentList 'open' } else { Start-Process 'http://127.0.0.1:8080/' }
  } finally {
    Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
  }
}
