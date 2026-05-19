package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"

	"slave-system/config"
	"slave-system/database"
	"slave-system/heartbeat"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load config
	err := config.LoadConfig("config/config.json")
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}

	// Connect to Database
	database.ConnectDB()

	// Start Heartbeat
	heartbeat.StartHeartbeat()

	// Setup Gin Router
	r := gin.Default()
	r.Use(cors.Default())

	// Serve Frontend
	r.Static("/gui", "./frontend")

	// API Endpoints
	r.GET("/clients", getClients)
	
	// Replicate endpoint called by Master
	r.POST("/replicate", handleReplication)
	
	// Forward write requests to Master
	r.POST("/insert", forwardRequest)
	r.PUT("/update", forwardRequest)
	r.DELETE("/delete", forwardRequest)
	
	// Status
	r.GET("/status", getStatus)

	addr := fmt.Sprintf("%s:%s", config.AppConfig.IP, config.AppConfig.Port)
	log.Printf("Slave Node starting on %s...", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}

func getClients(c *gin.Context) {
	clients, err := database.GetAllClients()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, clients)
}

func handleReplication(c *gin.Context) {
	var req struct {
		Action string                 `json:"action"`
		Table  string                 `json:"table"`
		Data   map[string]interface{} `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	log.Printf("Received replication action: %s on table: %s", req.Action, req.Table)

	if req.Table == "clients" {
		switch req.Action {
		case "insert":
			id := int(req.Data["id"].(float64))
			name := req.Data["name"].(string)
			balance := req.Data["balance"].(float64)
			city := req.Data["city"].(string)

			_, err := database.DB.Exec("INSERT INTO clients (id, name, balance, city) VALUES (?, ?, ?, ?)", id, name, balance, city)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		case "update":
			id := int(req.Data["id"].(float64))
			name := req.Data["name"].(string)
			balance := req.Data["balance"].(float64)
			city := req.Data["city"].(string)

			_, err := database.DB.Exec("UPDATE clients SET name=?, balance=?, city=? WHERE id=?", name, balance, city, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		case "delete":
			id := int(req.Data["id"].(float64))

			_, err := database.DB.Exec("DELETE FROM clients WHERE id=?", id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "Replication successful"})
}

func forwardRequest(c *gin.Context) {
	if config.AppConfig.Role == "master" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "This node is now a Master due to failover. Please use the Master Node APIs directly."})
		return
	}

	if heartbeat.IsMasterDown {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Master is currently down, cannot process writes."})
		return
	}

	masterURL := fmt.Sprintf("http://%s:%s%s", config.AppConfig.MasterIP, config.AppConfig.MasterPort, c.Request.URL.Path)
	log.Printf("Forwarding %s request to %s", c.Request.Method, masterURL)

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	req, err := http.NewRequest(c.Request.Method, masterURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create forward request"})
		return
	}
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to forward to master"})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
}

func getStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"role": "slave",
		"master_down": heartbeat.IsMasterDown,
	})
}
