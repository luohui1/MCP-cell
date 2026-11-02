# MCPcell installer for Windows without the desktop parts (no tray icon):
# downloads a release, installs it into ~/.mcpcell/bin with the command-line
# interface, turns autostart on, takes over the agents' MCPs and shows the
# status. Meant for one line, run by a person or an AI agent:
#
#   irm https://raw.githubusercontent.com/luohui1/MCP-cell/main/scripts/install.ps1 | iex
#
# Environment:
#   MCPCELL_VERSION=v0.6.1    a release tag instead of the latest
#   MCPCELL_NO_AUTOSTART=1    leave autostart as it is
#   MCPCELL_NO_TAKEOVER=1     install only, take nothing over
#   MCPCELL_HOME              MCPcell's data directory (default ~/.mcpcell)
#
# The script is ASCII on purpose: Windows PowerShell 5.1 reads a file
# without a BOM as ANSI. MCPcell itself prints its messages in Chinese.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    $repo = 'luohui1/MCP-cell'

    # x64 builds only; Windows on ARM runs them under emulation.
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $arch = $env:PROCESSOR_ARCHITEW6432 }
    if ($arch -ne 'AMD64' -and $arch -ne 'ARM64') {
        throw "MCPcell needs 64-bit Windows (this is $arch)."
    }
    # Windows' own tar: a GNU tar earlier on PATH (Git's usr\bin) reads
    # "D:\..." as a remote host.
    $tar = Join-Path $env:SystemRoot 'System32\tar.exe'
    if (-not (Test-Path -LiteralPath $tar)) {
        throw 'tar.exe was not found; it ships with Windows 10 1803 and later.'
    }

    $pkg = 'mcpcell-windows-amd64'
    if ($env:MCPCELL_VERSION) {
        $url = "https://github.com/$repo/releases/download/$($env:MCPCELL_VERSION)/$pkg.tar.gz"
    } else {
        $url = "https://github.com/$repo/releases/latest/download/$pkg.tar.gz"
    }
    $mcHome = $env:MCPCELL_HOME
    if (-not $mcHome) { $mcHome = Join-Path $env:USERPROFILE '.mcpcell' }
    $mcpcell = Join-Path $mcHome 'bin\mcpcell.exe'

    # A new, empty directory for the download, removed at the end.
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('mcpcell-install-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        Write-Host "Downloading $url"
        Invoke-WebRequest -Uri $url -OutFile (Join-Path $tmp "$pkg.tar.gz") -UseBasicParsing
        & $tar -xzf (Join-Path $tmp "$pkg.tar.gz") -C $tmp
        if ($LASTEXITCODE -ne 0) { throw "Could not unpack $pkg.tar.gz." }

        & (Join-Path $tmp "$pkg\mcpcell.exe") install --cli
        if ($LASTEXITCODE -ne 0) { throw 'mcpcell install failed.' }

        if ($env:MCPCELL_NO_AUTOSTART -ne '1') {
            & $mcpcell autostart on
            if ($LASTEXITCODE -ne 0) { Write-Warning 'Autostart was not turned on.' }
        }
        if ($env:MCPCELL_NO_TAKEOVER -ne '1') {
            & $mcpcell takeover -y
            if ($LASTEXITCODE -ne 0) { Write-Warning 'Some entries were not taken over; the reasons are listed above.' }
        }
        & $mcpcell status
        Write-Host ''
        Write-Host "MCPcell is installed in $(Split-Path $mcpcell). Start new agent conversations to use it."
        Write-Host "Undo: & '$mcpcell' restore --all"
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}
