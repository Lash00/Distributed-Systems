# Master Node - Distributed Bank

This is the **Master Node** for the simple distributed banking system built in Go.

## Architecture & Goroutines
- **Master Node**: Responsible for handling all WRITE operations (`INSERT`, `UPDATE`, `DELETE`).
- **Slaves**: Forward WRITEs to the master.
- **Replication Flow**: Whenever the master executes a write operation, it automatically replicates this change to all active slave nodes concurrently using **Goroutines**.
  ```go
  for _, slaveIP := range currentSlaves {
      go sendReplication(slaveIP, payload)
  }
  ```
  This guarantees that replication requests are sent out in parallel, demonstrating Go's concurrency.
- **Heartbeat Mechanism**: The master listens for heartbeat pulses from slaves to maintain a list of active slaves.

## Setup Instructions

1. Ensure you have **Go** and **MySQL** installed.
2. Ensure your MySQL server is running locally on port `3306`.
3. Open `config/config.json` and adjust the `db_user` and `db_pass` to match your MySQL credentials.
4. Run `run-master.bat` from the root folder, or navigate here and run `go run backend/main.go`.
5. Open your browser and navigate to `http://localhost:8080/gui` to view the beautiful modern dashboard.

> **Note**: The application will **automatically create** the database (`master_bank`) and the necessary tables on startup! No need to run SQL setup scripts manually!

## How to connect devices on LAN
If you want to run the master and slaves on different computers:
1. Find the Master computer's IPv4 address (e.g., `192.168.1.5`).
2. In the Master's `config.json`, change `ip` from `127.0.0.1` to `0.0.0.0` or `192.168.1.5`.
3. Give your friends the `slave-system` folder.
4. Tell them to open `slave-system/config/config.json` and change `master_ip` to `192.168.1.5`.

## API Documentation
- `GET /clients` : Retrieves all clients from the database.
- `POST /insert` : Inserts a new client.
- `PUT /update` : Updates an existing client.
- `DELETE /delete` : Deletes a client.
- `POST /text-to-query` : Natural language parsing for simple database queries.
