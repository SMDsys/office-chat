@echo off
chcp 65001 >nul
setlocal EnableExtensions
cd /d "%~dp0"
title Офис-чат — запуск вручную

if not exist "%~dp0OfficeChat.exe" (
  echo   Файл OfficeChat.exe не найден рядом с этим скриптом.
  pause
  exit /b 1
)

set "PORT="
if exist "%~dp0config.json" (
  for /f "tokens=2 delims=:," %%a in ('findstr /i "port" "%~dp0config.json" 2^>nul') do set "PORT=%%a"
)
if defined PORT set "PORT=%PORT: =%"
if not defined PORT set "PORT=8080"

echo.
echo   Запуск без службы — окно должно оставаться открытым.
echo   Остановить сервер: Ctrl+C или закрыть это окно.
echo.
echo   Адрес: http://localhost:%PORT%
echo.

"%~dp0OfficeChat.exe"
echo.
echo   Сервер остановлен.
pause
