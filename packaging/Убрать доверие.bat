@echo off
chcp 65001 >nul
setlocal EnableExtensions
title Офис-чат — убрать доверие сертификату

rem Удаляет сертификат сервера из «Доверенных корневых центров».
rem Нужен, если сертификат больше не используется или был добавлен по ошибке.

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

set "CERT=%~1"
if not defined CERT if exist "%~dp0cert.pem" set "CERT=%~dp0cert.pem"
if not defined CERT if exist "%~dp0data\tls\cert.pem" set "CERT=%~dp0data\tls\cert.pem"

if not defined CERT (
  echo.
  echo   Не нашёл файл cert.pem.
  echo   Положите cert.pem рядом с этим файлом или перетащите его на скрипт.
  echo.
  pause
  exit /b 1
)

if not exist "%CERT%" (
  echo.
  echo   Файл не найден: %CERT%
  echo.
  pause
  exit /b 1
)

for /f "usebackq delims=" %%t in (`powershell -NoProfile -Command "(Get-PfxCertificate -LiteralPath '%CERT%').Thumbprint"`) do set "THUMB=%%t"
if not defined THUMB (
  echo.
  echo   Не удалось прочитать сертификат.
  echo.
  pause
  exit /b 1
)

echo.
echo   Убираю из доверенных: %THUMB%
echo.
certutil -delstore Root "%THUMB%"
if errorlevel 1 (
  echo.
  echo   Сертификат не найден в «Доверенных корневых центрах»
  echo   или удалить его не удалось.
  echo.
  pause
  exit /b 1
)

echo.
echo   Готово. Браузер снова будет показывать предупреждение
echo   о сертификате при открытии чата по HTTPS.
echo.
pause
