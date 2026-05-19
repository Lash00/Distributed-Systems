@echo off
title Slave Node Runner
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

:: 2. Check if MySQL is running on port 3306 (Optional but helpful check)
echo [*] Checking database port 3306...
netstat -ano | findstr :3306 >nul 2>nul
if %errorlevel% neq 0 (
    echo [WARNING] MySQL database port (3306) does not seem to be active.
    echo           Please make sure your MySQL server is running (e.g. XAMPP, MySQL Service, or Docker).
    echo.
)

:: 3. Run the Slave Node
echo [*] Changing directory to slave-system...
cd slave-system

echo [*] Starting the Slave Node...
echo.
go run backend/main.go

if %errorlevel% neq 0 (
    echo.
    echo [ERROR] The Slave Node application exited with an error.
    echo         Common causes:
    echo         - MySQL database is not running
    echo         - Database credentials/port mismatch in slave-system/config/config.json
    echo         - Port is already in use by another application
    echo.
)

pause

