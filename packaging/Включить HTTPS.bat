@echo off
chcp 65001 >nul
setlocal EnableExtensions
cd /d "%~dp0"
title Офис-чат — включение HTTPS

net session >nul 2>&1
if errorlevel 1 (
  echo.
  echo   Нужны права администратора.
  echo   Закройте это окно, нажмите на файл правой кнопкой мыши
  echo   и выберите «Запуск от имени администратора».
  echo.
  pause
  exit /b 1
)

if not exist "%~dp0config.json" (
  echo.
  echo   Рядом нет config.json. Распакуйте архив целиком в отдельную папку.
  echo.
  pause
  exit /b 1
)

echo.
echo   Включаю HTTPS в config.json…
powershell -NoProfile -ExecutionPolicy Bypass -Command "$p = Join-Path '%~dp0' 'config.json'; $j = Get-Content -LiteralPath $p -Raw | ConvertFrom-Json; $j | Add-Member -NotePropertyName tls -NotePropertyValue $true -Force; $t = $j | ConvertTo-Json; [IO.File]::WriteAllText($p, $t, (New-Object System.Text.UTF8Encoding($false)))"
if errorlevel 1 (
  echo.
  echo   Не удалось изменить config.json. Откройте его в блокноте,
  echo   поставьте "tls": true и снова запустите install.bat.
  echo.
  pause
  exit /b 1
)

echo   Готово. Перезапускаю службу — адрес станет https://
echo.
call "%~dp0install.bat"
