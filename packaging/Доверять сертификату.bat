@echo off
chcp 65001 >nul
setlocal EnableExtensions
title Офис-чат — доверять сертификату

rem Добавляет сертификат сервера в «Доверенные корневые центры».
rem Запускать от имени администратора на каждом компьютере, где открывают чат.
rem Сертификат можно перетащить на этот файл или положить cert.pem рядом.

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
  echo.
  echo   На компьютере с сервером он лежит в папке data\tls\cert.pem.
  echo   Скопируйте его на этот компьютер и положите рядом с этим файлом
  echo   либо просто перетащите cert.pem на этот скрипт.
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

echo.
echo   Сертификат: %CERT%
echo   Добавляю в «Доверенные корневые центры»…
echo.
certutil -addstore -f Root "%CERT%"
if errorlevel 1 (
  echo.
  echo   Не удалось добавить сертификат.
  echo   Убедитесь, что это файл cert.pem с сервера Офис-чата.
  echo.
  pause
  exit /b 1
)

for /f "usebackq delims=" %%t in (`powershell -NoProfile -Command "(Get-PfxCertificate -LiteralPath '%CERT%').Thumbprint"`) do set "THUMB=%%t"

echo.
echo   ─────────────────────────────────────────
echo   Готово. Сертификат доверенный.
if defined THUMB echo   Отпечаток: %THUMB%
echo.
echo   Закройте все окна браузера и откройте чат заново.
echo   Адрес с https:// должен открыться с зелёным замком,
echo   а системные уведомления — заработать в Chrome и Edge.
echo.
echo   Если сервер пересоздаст сертификат (например, сменится
echo   IP-адрес), повторите этот шаг с новым cert.pem.
echo   ─────────────────────────────────────────
echo.
pause
