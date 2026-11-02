@echo off
chcp 65001 >nul
setlocal EnableExtensions
title Офис-чат

rem Адрес сервера.
rem На компьютере с сервером оставьте localhost.
rem Для сотрудников укажите адрес сервера, например:
rem   set "URL=http://192.168.1.10:8080"
rem Либо запустите этот файл с адресом:  Подключиться.bat http://192.168.1.10:8080

set "URL=%~1"

set "PORT="
if exist "%~dp0config.json" (
  for /f "tokens=2 delims=:," %%a in ('findstr /i "port" "%~dp0config.json" 2^>nul') do set "PORT=%%a"
)
if defined PORT set "PORT=%PORT: =%"
if not defined PORT set "PORT=8080"

set "TLSVAL="
if exist "%~dp0config.json" (
  for /f "tokens=2 delims=:," %%a in ('findstr /i /c:"tls" "%~dp0config.json" 2^>nul') do set "TLSVAL=%%a"
)
if defined TLSVAL set "TLSVAL=%TLSVAL: =%"
set "SCHEME=http"
if /i "%TLSVAL%"=="true" set "SCHEME=https"

if not defined URL set "URL=%SCHEME%://localhost:%PORT%"

rem Открываем в отдельном окне без адресной строки (Edge, затем Chrome),
rem если не получилось — в браузере по умолчанию.
start "" msedge --app="%URL%"
if not errorlevel 1 exit /b 0
start "" chrome --app="%URL%"
if not errorlevel 1 exit /b 0
start "" "%URL%"
