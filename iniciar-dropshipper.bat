@echo off
title Dropshipper - Motor + ngrok
echo ============================================
echo   Dropshipper Motor + tunel ngrok
echo   Panel:  http://localhost:8080
echo   Webhook: https://handlebar-nursing-stainless.ngrok-free.dev/webhook/whatsapp
echo ============================================
echo.
echo Cerrando motores anteriores...
taskkill /F /IM motor.exe >nul 2>&1
taskkill /F /IM ngrok.exe >nul 2>&1
timeout /t 2 >nul

echo Arrancando el Motor...
start "Motor Dropshipper" cmd /c "cd /d %~dp0motor && motor.exe"
timeout /t 3 >nul

echo Arrancando el tunel ngrok...
start "ngrok" cmd /c "cd /d C:\Users\EQUIPO\ngrok && ngrok.exe http 8080"

echo.
echo Listo. Abre el panel en http://localhost:8080
echo NO cierres las dos ventanas negras que acaban de abrirse.
echo.
pause
