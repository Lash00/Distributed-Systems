package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"master-system/config"
	"master-system/database"
	"master-system/heartbeat"
	"master-system/replication"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Load config
	err := config.LoadConfig("config/config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// 2. Connect to Database
	database.ConnectDB()

	// 3. Setup Gin Router
	r := gin.Default()
	r.Use(cors.Default())

	// 4. Serve Frontend
	r.Static("/gui", "./frontend")

	// 5. API Endpoints
	r.GET("/clients", getClients)
	r.POST("/insert", insertClient)
	r.PUT("/update", updateClient)
	r.DELETE("/delete", deleteClient)
	
	r.POST("/text-to-query", handleTextToQuery) // simple NLP to query

	// 6. Slave API Endpoints
	r.POST("/heartbeat", receiveHeartbeat)
	
	// 7. GUI Info Endpoints
	r.GET("/status", getStatus)

	addr := fmt.Sprintf("%s:%s", config.AppConfig.IP, config.AppConfig.Port)
	log.Printf("Master Node starting on %s...", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

// --- HANDLERS ---

func getClients(c *gin.Context) {
	clients, err := database.GetAllClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, clients)
}

func insertClient(c *gin.Context) {
	var req database.Client
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[ERROR] Failed to bind JSON for insert: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}
	
	log.Printf("[INFO] Received INSERT request: Name='%s', Balance=%0.2f, City='%s'", req.Name, req.Balance, req.City)

	// Insert into local DB
	res, err := database.DB.Exec("INSERT INTO clients (name, balance, city) VALUES (?, ?, ?)", req.Name, req.Balance, req.City)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	id, _ := res.LastInsertId()
	req.ID = int(id)

	// Replicate concurrently to all slaves
	replication.ReplicateToSlaves("insert", "clients", req)

	c.JSON(http.StatusOK, gin.H{"message": "Inserted successfully", "id": req.ID})
}

func updateClient(c *gin.Context) {
	var req database.Client
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[ERROR] Failed to bind JSON for update: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}
	
	log.Printf("[INFO] Received UPDATE request: ID=%d, Name='%s', Balance=%0.2f, City='%s'", req.ID, req.Name, req.Balance, req.City)

	// Update local DB
	_, err := database.DB.Exec("UPDATE clients SET name=?, balance=?, city=? WHERE id=?", req.Name, req.Balance, req.City, req.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Replicate concurrently to all slaves
	replication.ReplicateToSlaves("update", "clients", req)

	c.JSON(http.StatusOK, gin.H{"message": "Updated successfully"})
}

func deleteClient(c *gin.Context) {
	var req struct {
		ID int `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Delete from local DB
	_, err := database.DB.Exec("DELETE FROM clients WHERE id=?", req.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Replicate concurrently to all slaves
	replication.ReplicateToSlaves("delete", "clients", map[string]interface{}{"id": req.ID})

	c.JSON(http.StatusOK, gin.H{"message": "Deleted successfully"})
}

// Heartbeat from slaves
func receiveHeartbeat(c *gin.Context) {
	var req struct {
		Address string `json:"address"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}
	
	// Get the port of the slave
	parts := strings.Split(req.Address, ":")
	port := "8081"
	if len(parts) > 1 {
		port = parts[len(parts)-1]
	}

	// Resolve the real client IP (caller IP)
	clientIP := c.ClientIP()
	
	// If the slave sent a loopback/local address, substitute it with their actual network IP
	var realAddress string
	if strings.HasPrefix(req.Address, "127.0.0.1") || strings.HasPrefix(req.Address, "localhost") || strings.HasPrefix(req.Address, "0.0.0.0") || strings.HasPrefix(req.Address, "::1") {
		realAddress = fmt.Sprintf("%s:%s", clientIP, port)
	} else {
		realAddress = req.Address
	}
	
	log.Printf("Heartbeat received from %s (registered as %s)", req.Address, realAddress)
	
	// Register the slave's heartbeat
	heartbeat.RegisterSlave(realAddress)
	c.JSON(http.StatusOK, gin.H{"message": "Heartbeat acknowledged"})
}

// Simple Text to Query parser
func handleTextToQuery(c *gin.Context) {
	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	text := strings.ToLower(strings.TrimSpace(req.Text))
	parts := strings.Split(text, " ")

	if strings.HasPrefix(text, "delete client") && len(parts) >= 3 {
		// e.g. "delete client 5"
		idStr := parts[2]
		id, _ := strconv.Atoi(idStr)
		
		_, err := database.DB.Exec("DELETE FROM clients WHERE id=?", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		replication.ReplicateToSlaves("delete", "clients", map[string]interface{}{"id": id})
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Deleted client %d successfully.", id)})
		return
	}

	if strings.HasPrefix(text, "withdraw") && len(parts) >= 5 {
		// e.g. "withdraw 500 from client 7"
		amountStr := parts[1]
		amount, err := strconv.ParseFloat(amountStr, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid amount"})
			return
		}

		idStr := parts[4]
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid client ID"})
			return
		}

		// Get current client
		var client database.Client
		err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client %d not found", id)})
			return
		}

		if client.Balance < amount {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Insufficient balance"})
			return
		}

		newBalance := client.Balance - amount
		_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		client.Balance = newBalance
		replication.ReplicateToSlaves("update", "clients", client)

		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Withdrew $%0.2f from Client %d (%s). New balance: $%0.2f.", amount, id, client.Name, newBalance)})
		return
	}

	if (strings.HasPrefix(text, "show all clients in") || strings.HasPrefix(text, "clients in")) && len(parts) >= 4 {
		// e.g. "show all clients in Cairo"
		// parts: [show, all, clients, in, cairo] -> len is 5
		city := parts[len(parts)-1]
		cityTitle := strings.Title(city)

		rows, err := database.DB.Query("SELECT id, name, balance, city FROM clients WHERE LOWER(city)=?", city)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		var results []string
		for rows.Next() {
			var client database.Client
			if err := rows.Scan(&client.ID, &client.Name, &client.Balance, &client.City); err == nil {
				results = append(results, fmt.Sprintf("%s (ID: %d, Balance: $%0.2f)", client.Name, client.ID, client.Balance))
			}
		}

		if len(results) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("No clients found in %s.", cityTitle)})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Clients found in %s: %s", cityTitle, strings.Join(results, ", "))})
		return
	}

	c.JSON(http.StatusBadRequest, gin.H{"error": "Could not parse query. Examples: 'delete client 5', 'withdraw 500 from client 7', 'show all clients in Cairo'."})
}

func getStatus(c *gin.Context) {
	activeSlaves := heartbeat.GetActiveSlaves()
	c.JSON(http.StatusOK, gin.H{
		"role": "master",
		"active_slaves": activeSlaves,
	})
}
