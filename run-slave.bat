@echo off
echo Starting Slave Node...
cd slave-system
go run backend/main.go
pause
