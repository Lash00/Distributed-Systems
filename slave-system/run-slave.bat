@echo off
title Slave Node Runner (Internal)
echo ==========================================
echo       STARTING BANKING SYSTEM: SLAVE      
echo ==========================================
echo.

:: 1. Check if Go is installed and available
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Go (golang) is not found in your system's PATH.
    echo.
    echo Please make sure Go is installed:
    echo 1. Download it from: https://go.dev/dl/
    echo 2. Verify installation by running 'go version' in a new Command Prompt.
    echo.
    pause
    exit /b
)

:: 2. Run the Slave Node
echo [*] Starting the Slave Node...
echo.
go run backend/main.go

if %errorlevel% neq 0 (
    echo.
    echo [ERROR] The Slave Node application exited with an error.
    echo         Common causes:
    echo         - MySQL database is not running
    echo         - Database credentials/port mismatch in config/config.json
    echo         - Port is already in use by another application
    echo.
)

pause

