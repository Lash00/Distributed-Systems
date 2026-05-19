package database

import (
	"database/sql"
	"fmt"
	"log"

	"master-system/config"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

// ConnectDB establishes a connection to the MySQL database
func ConnectDB() {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/",
		config.AppConfig.DBUser,
		config.AppConfig.DBPass,
		config.AppConfig.DBHost,
		config.AppConfig.DBPort,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to MySQL: %v", err)
	}

	// Create database if it doesn't exist
	_, err = db.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", config.AppConfig.DBName))
	if err != nil {
		log.Fatalf("Failed to create database: %v", err)
	}

	db.Close()

	// Connect to the specific database
	dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
		config.AppConfig.DBUser,
		config.AppConfig.DBPass,
		config.AppConfig.DBHost,
		config.AppConfig.DBPort,
		config.AppConfig.DBName,
	)

	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	err = DB.Ping()
	if err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	log.Println("Successfully connected to the database!")

	// Create clients table if it doesn't exist
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS clients (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		balance DECIMAL(10, 2) NOT NULL,
		city VARCHAR(255) NOT NULL
	);`
	_, err = DB.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("Failed to create clients table: %v", err)
	}
}

// Client represents a client record
type Client struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Balance float64 `json:"balance"`
	City    string  `json:"city"`
}

func GetAllClients() ([]Client, error) {
	rows, err := DB.Query("SELECT id, name, balance, city FROM clients")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		if err := rows.Scan(&c.ID, &c.Name, &c.Balance, &c.City); err != nil {
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, nil
}
