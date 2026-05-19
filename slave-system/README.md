# Slave Node - Distributed Bank

This is the **Slave Node** for the simple distributed banking system built in Go.

## Overview
- The slave acts as a read-only replica.
- It maintains its own local MySQL database (`slave_bank_1`).
- All `INSERT`, `UPDATE`, and `DELETE` requests submitted via the Slave GUI are **forwarded** over the network to the Master node.
- The master then executes the request and asynchronously replicates the changes back to all slaves (including this one).

## Failover & Heartbeat
- The slave uses a goroutine to continuously ping the master (Heartbeat) every 5 seconds.
- If the master stops responding, the slave GUI will indicate that the Master is offline, and write operations will be temporarily disabled until the master comes back online.

## Setup Instructions

1. Ensure you have **Go** and **MySQL** installed.
2. Ensure your MySQL server is running locally on port `3306`.
3. Open `config/config.json` and adjust the `db_user` and `db_pass` to match your MySQL credentials.
4. If the Master is on another PC, change `master_ip` in `config/config.json` to the Master's LAN IP.
5. Run `run-slave.bat` from the root folder, or navigate here and run `go run backend/main.go`.
6. Open your browser and navigate to `http://localhost:8081/gui` to view the beautiful modern dashboard.

> **Note**: The application will **automatically create** the database (`slave_bank_1`) and the necessary tables on startup! No need to run SQL setup scripts manually!
