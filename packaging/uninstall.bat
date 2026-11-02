@echo off
chcp 65001 >nul
setlocal EnableExtensions
cd /d "%~dp0"
title Удаление «Офис-чат»

net session >nul 2>&1
if errorlevel 1 (
  echo.
  echo   Нужны права администратора.
  echo   Нажмите на uninstall.bat правой кнопкой мыши
  echo   и выберите «Запуск от имени администратора».
  echo.
  pause
  exit /b 1
)

if not exist "%~dp0OfficeChat.exe" (
  echo   Файл OfficeChat.exe не найден. Удаление всё равно продолжится.
)

echo.
echo   Останавливаю и удаляю службу «Офис-чат»…
echo.
if exist "%~dp0OfficeChat.exe" "%~dp0OfficeChat.exe" -uninstall

netsh advfirewall firewall delete rule name="Офис-чат" >nul 2>&1

echo.
set "DEL="
set /p "DEL=Удалить данные, историю и резервные копии (папка data)? [y/N] "
if /i "%DEL%"=="y" (
  if exist "%~dp0data" (
    rd /s /q "%~dp0data"
    echo   Папка data удалена.
  ) else (
    echo   Папки data нет — удалять нечего.
  )
) else (
  echo   Данные сохранены в папке data.
)

echo.
echo   Служба «Офис-чат» удалена.
echo.
pause
