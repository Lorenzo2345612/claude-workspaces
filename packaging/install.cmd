@echo off
rem Doble clic: instala cw (Claude Code workspaces) en WSL.
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1" %*
