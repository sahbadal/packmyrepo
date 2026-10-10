# packmyrepo installer for Windows.
#   irm https://raw.githubusercontent.com/sahbadal/packmyrepo/main/install.ps1 | iex
# Options:
#   $env:PACKMYREPO_VERSION = "v0.1.0"   install a specific release (default: latest)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"   # makes Invoke-WebRequest much faster
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Repo = "sahbadal/packmyrepo"
$Name = "packmyrepo"
$Version = if ($env:PACKMYREPO_VERSION) { $env:PACKMYREPO_VERSION } else { "latest" }
$InstallDir = Join-Path $env:LOCALAPPDATA "packmyrepo\bin"

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  "AMD64" { "amd64" }
  "ARM64" { "arm64" }
  default { throw "packmyrepo: unsupported CPU $($env:PROCESSOR_ARCHITECTURE)" }
}

if ($Version -eq "latest") {
  $base = "https://github.com/$Repo/releases/latest/download"
} else {
  $base = "https://github.com/$Repo/releases/download/$Version"
}
$archive = "${Name}_windows_${arch}.zip"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("packmyrepo-" + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
  Write-Host "Downloading $archive ($Version)..."
  Invoke-WebRequest "$base/$archive" -OutFile (Join-Path $tmp $archive) -UseBasicParsing
  Invoke-WebRequest "$base/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt") -UseBasicParsing

  # Verify the download before running anything from it.
  $line = Get-Content (Join-Path $tmp "checksums.txt") | Where-Object { $_ -match " $([regex]::Escape($archive))$" }
  if (-not $line) { throw "packmyrepo: no checksum listed for $archive" }
  $expected = ($line -split "\s+")[0].ToLower()
  $actual = (Get-FileHash (Join-Path $tmp $archive) -Algorithm SHA256).Hash.ToLower()
  if ($expected -ne $actual) { throw "packmyrepo: checksum mismatch, the download may be corrupted. Nothing was installed." }

  Expand-Archive (Join-Path $tmp $archive) -DestinationPath $tmp -Force
  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
  Copy-Item (Join-Path $tmp "$Name.exe") (Join-Path $InstallDir "$Name.exe") -Force
} finally {
  Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# Add to the user's PATH (no admin rights needed).
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ";") -notcontains $InstallDir) {
  $newPath = if ($userPath) { "$userPath;$InstallDir" } else { $InstallDir }
  [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
  $env:Path = "$env:Path;$InstallDir"
  Write-Host "Added $InstallDir to your PATH."
}

$installed = & (Join-Path $InstallDir "$Name.exe") version
Write-Host "Installed $installed"
Write-Host "Open a new terminal and run: packmyrepo --help"