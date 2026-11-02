@echo off
chcp 65001 >nul
setlocal EnableExtensions
cd /d "%~dp0"
title Установка «Офис-чат»

net session >nul 2>&1
if errorlevel 1 (
  echo.
  echo   Нужны права администратора.
  echo   Закройте это окно, нажмите на install.bat правой кнопкой мыши
  echo   и выберите «Запуск от имени администратора».
  echo.
  pause
  exit /b 1
)

if not exist "%~dp0OfficeChat.exe" (
  echo.
  echo   Рядом со скриптом нет файла OfficeChat.exe.
  echo   Распакуйте архив целиком в отдельную папку, например C:\OfficeChat,
  echo   и запустите install.bat оттуда.
  echo.
  pause
  exit /b 1
)

rem --- порт из config.json (по умолчанию 8080) ---
set "PORT="
if exist "%~dp0config.json" (
  for /f "tokens=2 delims=:," %%a in ('findstr /i "port" "%~dp0config.json" 2^>nul') do set "PORT=%%a"
)
if defined PORT set "PORT=%PORT: =%"
if not defined PORT set "PORT=8080"

rem --- HTTPS включён? (tls: true в config.json) ---
set "TLSVAL="
if exist "%~dp0config.json" (
  for /f "tokens=2 delims=:," %%a in ('findstr /i /c:"tls" "%~dp0config.json" 2^>nul') do set "TLSVAL=%%a"
)
if defined TLSVAL set "TLSVAL=%TLSVAL: =%"
set "SCHEME=http"
if /i "%TLSVAL%"=="true" set "SCHEME=https"

echo.
echo   Устанавливаю службу «Офис-чат»…
echo.
"%~dp0OfficeChat.exe" -install
if errorlevel 1 (
  echo.
  echo   Не удалось установить службу. Подробности выше.
  echo   Проверьте, что папка доступна для записи и порт %PORT% свободен.
  echo.
  pause
  exit /b 1
)

echo.
echo   Открываю порт %PORT% в брандмауэре Windows…
netsh advfirewall firewall delete rule name="Офис-чат" >nul 2>&1
netsh advfirewall firewall add rule name="Офис-чат" dir=in action=allow protocol=TCP localport=%PORT% >nul
if errorlevel 1 (
  echo   Не удалось добавить правило брандмауэра. Добавьте его вручную
  echo   или разрешите OfficeChat.exe при первом запросе системы.
)

echo.
echo   ─────────────────────────────────────────
echo   Готово! Служба «Офис-чат» установлена,
echo   запущена и будет стартовать вместе с Windows.
echo.
echo   На этом компьютере:  %SCHEME%://localhost:%PORT%
for /f "tokens=2 delims=:" %%i in ('ipconfig ^| findstr /c:"IPv4"') do (
  for /f "tokens=* delims= " %%j in ("%%i") do echo   В локальной сети:    %SCHEME%://%%j:%PORT%
)
echo.
echo   Откройте адрес в браузере и зарегистрируйте первый
echo   аккаунт — он станет администратором.
echo   Остальным сотрудникам дайте адрес «В локальной сети».
echo   ─────────────────────────────────────────
echo.
pause
