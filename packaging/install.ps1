# Instalador de cw para Windows. cw corre dentro de WSL (Linux), así que este script:
#   1. comprueba que WSL tenga una distribución instalada (si no, la instala: pide permisos
#      de administrador y luego hay que reiniciar y volver a correr el instalador);
#   2. corre install.sh dentro de WSL (instala tmux, cw y, si quieres, Claude Code);
#   3. agrega a Windows Terminal un perfil «cw» que abre cw directamente;
#   4. crea los accesos directos «Agent Workspaces» (menú Inicio y escritorio) que abren la app.
#
# Uso: doble clic en install.cmd, o en PowerShell:  powershell -ExecutionPolicy Bypass -File install.ps1
param([switch]$Yes, [switch]$NoClaude)

$ErrorActionPreference = 'Stop'
function Say($m)  { Write-Host "==> $m" -ForegroundColor Cyan }
function Warn($m) { Write-Host " aviso: $m" -ForegroundColor Yellow }
function Fail($m) { Write-Host " error: $m" -ForegroundColor Red; Read-Host 'Enter para cerrar' | Out-Null; exit 1 }

if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
    Fail 'Este Windows no tiene WSL. Necesitas Windows 10 2004+ o Windows 11.'
}

# wsl.exe -l escribe en UTF-16: quitamos los NUL para poder leerlo.
$distros = @((& wsl.exe -l -q) -replace "`0", '' | Where-Object { $_.Trim() -ne '' })
if ($distros.Count -eq 0) {
    Warn 'WSL no tiene ninguna distribución de Linux instalada.'
    $ok = $Yes -or ((Read-Host '¿Instalar Ubuntu en WSL ahora? Pide permisos de administrador (S/n)') -notmatch '^[nN]')
    if (-not $ok) { Fail 'cw necesita WSL con una distribución de Linux.' }
    Start-Process wsl.exe -ArgumentList '--install', '-d', 'Ubuntu' -Verb RunAs -Wait
    Say 'Ubuntu se está instalando. Reinicia Windows si te lo pide, abre «Ubuntu» una vez para crear tu usuario'
    Say 'y vuelve a correr este instalador (install.cmd).'
    Read-Host 'Enter para cerrar' | Out-Null
    exit 0
}

$distro = ((& wsl.exe -e printenv WSL_DISTRO_NAME) -replace "`0", '' | Out-String).Trim()
if (-not $distro) { Fail 'No pude abrir la distribución de WSL por defecto. Ábrela una vez desde el menú Inicio y reintenta.' }
Say "Instalando dentro de WSL ($distro)"

$here = ((& wsl.exe -e wslpath -a "$PSScriptRoot") -replace "`0", '' | Out-String).Trim()
$flags = @()
if ($Yes) { $flags += '--yes' }
if ($NoClaude) { $flags += '--no-claude' }
& wsl.exe -d $distro -e sh "$here/install.sh" @flags
if ($LASTEXITCODE -ne 0) { Fail 'La instalación dentro de WSL no terminó bien (mira los mensajes de arriba).' }

# Perfil de Windows Terminal (un «fragment»: no toca tu settings.json).
$fragmentDir = Join-Path $env:LOCALAPPDATA 'Microsoft\Windows Terminal\Fragments\cw'
New-Item -ItemType Directory -Force -Path $fragmentDir | Out-Null
$wtProfile = @{
    profiles = @(@{
        name        = 'cw'
        commandline = "wsl.exe -d $distro --cd ~ -- bash -lic cw"
        icon        = "$([char]0x2261)"
        tabTitle    = 'Agent Workspaces terminal'
        suppressApplicationTitle = $false
    })
}
$json = $wtProfile | ConvertTo-Json -Depth 5
[System.IO.File]::WriteAllText((Join-Path $fragmentDir 'cw.json'), $json, (New-Object System.Text.UTF8Encoding $false))
Say 'Perfil «cw» agregado a Windows Terminal (menú ˅ de pestañas nuevas).'

# Solo el paquete de escritorio («Agent Workspaces») trae cw.ico: el de terminal no crea accesos.
$desktop = Test-Path (Join-Path $PSScriptRoot 'cw.ico')
if ($desktop) {
    # Accesos directos de la app: un .vbs abre wsl.exe sin ventana de consola, y cw app --mantener
    # deja corriendo el lado Linux (servidor y sesiones) mientras la app está abierta.
    $appDir = Join-Path $env:LOCALAPPDATA 'cw'
    New-Item -ItemType Directory -Force -Path $appDir | Out-Null
    # Explorer caches a shortcut's icon by file path: a name that changes with the content makes a
    # new logo show up instead of the cached old one.
    $icoHash = (Get-FileHash (Join-Path $PSScriptRoot 'cw.ico') -Algorithm SHA256).Hash.Substring(0, 8).ToLower()
    $ico = Join-Path $appDir "cw-$icoHash.ico"
    Get-ChildItem $appDir -Filter 'cw*.ico' -ErrorAction SilentlyContinue | Where-Object { $_.FullName -ne $ico } | Remove-Item -Force -ErrorAction SilentlyContinue
    Copy-Item -Force (Join-Path $PSScriptRoot 'cw.ico') $ico
    $vbs = Join-Path $appDir 'cw-app.vbs'
    $vbsText = "Set sh = CreateObject(""WScript.Shell"")`r`n" +
        "sh.Run ""wsl.exe -d $distro --cd ~ -- bash -lc """"exec `$HOME/.local/bin/cw app --mantener"""""", 0, False`r`n"
    [System.IO.File]::WriteAllText($vbs, $vbsText, (New-Object System.Text.UTF8Encoding $false))
    $shell = New-Object -ComObject WScript.Shell
    foreach ($dir in @([Environment]::GetFolderPath('Programs'), [Environment]::GetFolderPath('Desktop'))) {
        $oldShortcut = Join-Path $dir 'Claude Workspaces.lnk'
        $newShortcut = Join-Path $dir 'Agent Workspaces.lnk'
        if ((Test-Path $oldShortcut) -and -not (Test-Path $newShortcut)) {
            Move-Item -LiteralPath $oldShortcut -Destination $newShortcut
        }
        $lnk = $shell.CreateShortcut($newShortcut)
        $lnk.TargetPath = Join-Path $env:WINDIR 'System32\wscript.exe'
        $lnk.Arguments = '"' + $vbs + '"'
        $lnk.IconLocation = $ico
        $lnk.Description = 'Sesiones de Claude Code, Codex y terminal en pestañas'
        $lnk.Save()
    }
    & (Join-Path $env:WINDIR 'System32\ie4uinit.exe') -show 2>$null # refresh Explorer's icon cache
    Say 'Accesos directos «Agent Workspaces» creados en el menú Inicio y el escritorio.'
}

if ($desktop) {
    Say 'Listo. Abre «Agent Workspaces» desde el menú Inicio, o el perfil «cw» de Windows Terminal para la versión de terminal.'
} else {
    Say 'Listo. Abre Windows Terminal y elige el perfil «cw», o escribe cw en tu terminal de Ubuntu.'
}
Read-Host 'Enter para cerrar' | Out-Null
