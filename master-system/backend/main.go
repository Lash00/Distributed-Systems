package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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

	// 8. Client Change Request Queue Endpoints
	r.POST("/submit-request", submitChangeRequest)
	r.GET("/pending-requests", getPendingRequests)
	r.POST("/approve-request", approveChangeRequest)
	r.POST("/reject-request", rejectChangeRequest)

	// 9. Failback Sync Endpoint — called by demoting temp-slave
	r.POST("/sync-from-slave", syncFromSlave)

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
	
	if err := ValidateClientData(req.Name, req.Balance, req.City); err != nil {
		log.Printf("[ERROR] Validation failed for insert: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
	
	if err := ValidateClientData(req.Name, req.Balance, req.City); err != nil {
		log.Printf("[ERROR] Validation failed for update: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	log.Printf("[INFO] Received UPDATE request: ID=%d, Name='%s', Balance=%0.2f, City='%s'", req.ID, req.Name, req.Balance, req.City)

	// Update local DB
	res, err := database.DB.Exec("UPDATE clients SET name=?, balance=?, city=? WHERE id=?", req.Name, req.Balance, req.City, req.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d does not exist", req.ID)})
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
	res, err := database.DB.Exec("DELETE FROM clients WHERE id=?", req.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d does not exist", req.ID)})
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

	if strings.HasPrefix(text, "delete client") {
		id, err := parseDeleteQuery(text)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse delete query: " + err.Error()})
			return
		}
		
		res, err := database.DB.Exec("DELETE FROM clients WHERE id=?", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
			return
		}
		replication.ReplicateToSlaves("delete", "clients", map[string]interface{}{"id": id})
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Deleted client %d successfully.", id)})
		return
	}

	if strings.HasPrefix(text, "withdraw") {
		amount, id, err := parseWithdrawQuery(text)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse withdraw query: " + err.Error()})
			return
		}

		// Get current client
		var client database.Client
		err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
			return
		}

		if client.Balance < amount {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Insufficient balance. Client %d has $%0.2f, cannot withdraw $%0.2f.", id, client.Balance, amount)})
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

	if strings.HasPrefix(text, "deposit") {
		amount, id, err := parseDepositQuery(text)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse deposit query: " + err.Error()})
			return
		}

		var client database.Client
		err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
			return
		}

		newBalance := client.Balance + amount
		_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		client.Balance = newBalance
		replication.ReplicateToSlaves("update", "clients", client)

		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Deposited $%0.2f to Client %d (%s). New balance: $%0.2f.", amount, id, client.Name, newBalance)})
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
	syncMu.Lock()
	sc := SyncComplete
	syncMu.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"role":          "master",
		"active_slaves": activeSlaves,
		"sync_complete": sc,
	})
}

// syncFromSlave is called by the demoting temp-slave on failback.
// It receives the slave's full DB state + pending requests and merges them into master.
func syncFromSlave(c *gin.Context) {
	var body struct {
		Clients         []database.Client `json:"clients"`
		PendingRequests []ChangeRequest   `json:"pending_requests"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		log.Printf("[SYNC] Failed to bind sync payload: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid sync payload: " + err.Error()})
		return
	}

	log.Printf("[SYNC] Received failback sync from slave: %d clients, %d pending requests", len(body.Clients), len(body.PendingRequests))

	// 1. Upsert all clients from the slave into master DB
	for _, client := range body.Clients {
		// Try update first
		res, err := database.DB.Exec(
			"UPDATE clients SET name=?, balance=?, city=? WHERE id=?",
			client.Name, client.Balance, client.City, client.ID,
		)
		if err != nil {
			log.Printf("[SYNC] Error upserting client ID=%d: %v", client.ID, err)
			continue
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			// Row didn't exist, insert with explicit ID
			_, err = database.DB.Exec(
				"INSERT INTO clients (id, name, balance, city) VALUES (?, ?, ?, ?)",
				client.ID, client.Name, client.Balance, client.City,
			)
			if err != nil {
				log.Printf("[SYNC] Error inserting new client ID=%d: %v", client.ID, err)
			}
		}
	}
	log.Printf("[SYNC] Upserted %d clients from temp-slave into master DB", len(body.Clients))

	// 2. Merge pending requests from the temp-slave into master's queue
	reqMu.Lock()
	for _, pr := range body.PendingRequests {
		// Tag it so master GUI can show it as inherited
		pr.ID = "INHERITED-" + pr.ID
		pr.Status = "pending"
		PendingRequests = append(PendingRequests, pr)
		log.Printf("[SYNC] Inherited pending request: %s (type=%s)", pr.ID, pr.Type)
	}
	reqMu.Unlock()

	// 3. Replicate the full updated DB to all active slaves for uniformity
	go func() {
		allClients, err := database.GetAllClients()
		if err != nil {
			log.Printf("[SYNC] Failed to fetch clients for post-sync replication: %v", err)
			return
		}
		for _, client := range allClients {
			replication.ReplicateToSlaves("update", "clients", client)
		}
		log.Printf("[SYNC] Post-sync replication sent to all active slaves")
	}()

	// 4. Mark sync as complete
	syncMu.Lock()
	SyncComplete = true
	syncMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"message": "Sync received and applied successfully"})
}

// ChangeRequest represents an incoming write request from a slave or client
type ChangeRequest struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"` // "insert" or "update"
	Data      map[string]interface{} `json:"data"`
	Status    string                 `json:"status"` // "pending", "approved", "rejected"
	OriginIP  string                 `json:"origin_ip"`
	Timestamp string                 `json:"timestamp"`
}

var PendingRequests = []ChangeRequest{}
var reqMu sync.Mutex

// SyncComplete tracks whether the master has received a full sync from the temp-slave after failback
var SyncComplete bool
var syncMu sync.Mutex

func submitChangeRequest(c *gin.Context) {
	var req struct {
		Type string                 `json:"type"`
		Data map[string]interface{} `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("[ERROR] Failed to bind JSON for submit request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	// Validate data contents if inserting/updating
	if req.Type == "insert" || req.Type == "update" {
		name, _ := req.Data["name"].(string)
		var balance float64
		if bVal, ok := req.Data["balance"]; ok {
			switch v := bVal.(type) {
			case float64:
				balance = v
			case int:
				balance = float64(v)
			}
		}
		city, _ := req.Data["city"].(string)
		if err := ValidateClientData(name, balance, city); err != nil {
			log.Printf("[ERROR] Proposal validation failed: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Validation failed: " + err.Error()})
			return
		}
	}

	// Validate client ID existence for update/delete/withdraw/deposit
	if req.Type == "update" {
		idVal, ok := req.Data["id"]
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Client ID is required for update"})
			return
		}
		var targetID int
		switch v := idVal.(type) {
		case float64:
			targetID = int(v)
		case int:
			targetID = v
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Client ID format"})
			return
		}

		var exists bool
		err := database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM clients WHERE id=?)", targetID).Scan(&exists)
		if err != nil || !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d does not exist in Master DB", targetID)})
			return
		}
	} else if req.Type == "smart_query" {
		queryText, ok := req.Data["query"].(string)
		if ok {
			text := strings.ToLower(strings.TrimSpace(queryText))
			var targetID int
			var err error
			var isWithdraw, isDeposit, isDelete bool

			if strings.HasPrefix(text, "withdraw") {
				_, targetID, err = parseWithdrawQuery(text)
				isWithdraw = true
			} else if strings.HasPrefix(text, "deposit") {
				_, targetID, err = parseDepositQuery(text)
				isDeposit = true
			} else if strings.HasPrefix(text, "delete client") {
				targetID, err = parseDeleteQuery(text)
				isDelete = true
			}

			if (isWithdraw || isDeposit || isDelete) {
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid query syntax: " + err.Error()})
					return
				}
				var exists bool
				dbErr := database.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM clients WHERE id=?)", targetID).Scan(&exists)
				if dbErr != nil || !exists {
					c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d does not exist in Master DB", targetID)})
					return
				}
			}
		}
	}

	reqMu.Lock()
	defer reqMu.Unlock()

	id := fmt.Sprintf("REQ-%d", time.Now().UnixNano())
	newReq := ChangeRequest{
		ID:        id,
		Type:      req.Type,
		Data:      req.Data,
		Status:    "pending",
		OriginIP:  c.ClientIP(),
		Timestamp: time.Now().Format("15:04:05"),
	}

	PendingRequests = append(PendingRequests, newReq)
	log.Printf("[INFO] New change request received: %s from %s (Type: %s)", id, newReq.OriginIP, newReq.Type)

	c.JSON(http.StatusOK, gin.H{"message": "Request submitted to Master successfully", "id": id})
}

func getPendingRequests(c *gin.Context) {
	reqMu.Lock()
	defer reqMu.Unlock()
	c.JSON(http.StatusOK, PendingRequests)
}

func approveChangeRequest(c *gin.Context) {
	var body struct {
		ID string `json:"id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	reqMu.Lock()
	defer reqMu.Unlock()

	var targetReq *ChangeRequest
	var targetIndex = -1
	for i, r := range PendingRequests {
		if r.ID == body.ID {
			targetReq = &PendingRequests[i]
			targetIndex = i
			break
		}
	}

	if targetReq == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
		return
	}

	// Process the change on Master DB
	if targetReq.Type == "insert" {
		name := targetReq.Data["name"].(string)
		balance := targetReq.Data["balance"].(float64)
		city := targetReq.Data["city"].(string)

		res, err := database.DB.Exec("INSERT INTO clients (name, balance, city) VALUES (?, ?, ?)", name, balance, city)
		if err != nil {
			log.Printf("[ERROR] Failed to execute insert from request: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write to Master DB: " + err.Error()})
			return
		}

		id, _ := res.LastInsertId()
		// Replicate to all slaves
		clientData := database.Client{
			ID:      int(id),
			Name:    name,
			Balance: balance,
			City:    city,
		}
		replication.ReplicateToSlaves("insert", "clients", clientData)

	} else if targetReq.Type == "update" {
		id := int(targetReq.Data["id"].(float64))
		name := targetReq.Data["name"].(string)
		balance := targetReq.Data["balance"].(float64)
		city := targetReq.Data["city"].(string)

		res, err := database.DB.Exec("UPDATE clients SET name=?, balance=?, city=? WHERE id=?", name, balance, city, id)
		if err != nil {
			log.Printf("[ERROR] Failed to execute update from request: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write to Master DB: " + err.Error()})
			return
		}
		rowsAffected, _ := res.RowsAffected()
		if rowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found in Master DB", id)})
			return
		}

		// Replicate to all slaves
		clientData := database.Client{
			ID:      id,
			Name:    name,
			Balance: balance,
			City:    city,
		}
		replication.ReplicateToSlaves("update", "clients", clientData)
	} else if targetReq.Type == "smart_query" {
		queryText := targetReq.Data["query"].(string)
		text := strings.ToLower(strings.TrimSpace(queryText))

		if strings.HasPrefix(text, "withdraw") {
			amount, id, err := parseWithdrawQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse withdraw query: " + err.Error()})
				return
			}

			var client database.Client
			err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			if client.Balance < amount {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Insufficient balance. Client %d has $%0.2f, cannot withdraw $%0.2f.", id, client.Balance, amount)})
				return
			}

			newBalance := client.Balance - amount
			_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute withdrawal on Master DB: " + err.Error()})
				return
			}

			client.Balance = newBalance
			replication.ReplicateToSlaves("update", "clients", client)
		} else if strings.HasPrefix(text, "deposit") {
			amount, id, err := parseDepositQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse deposit query: " + err.Error()})
				return
			}

			var client database.Client
			err = database.DB.QueryRow("SELECT id, name, balance, city FROM clients WHERE id=?", id).Scan(&client.ID, &client.Name, &client.Balance, &client.City)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}

			newBalance := client.Balance + amount
			_, err = database.DB.Exec("UPDATE clients SET balance=? WHERE id=?", newBalance, id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute deposit on Master DB: " + err.Error()})
				return
			}

			client.Balance = newBalance
			replication.ReplicateToSlaves("update", "clients", client)
		} else if strings.HasPrefix(text, "delete client") {
			id, err := parseDeleteQuery(text)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse delete query: " + err.Error()})
				return
			}

			res, err := database.DB.Exec("DELETE FROM clients WHERE id=?", id)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to execute delete on Master DB: " + err.Error()})
				return
			}
			rowsAffected, _ := res.RowsAffected()
			if rowsAffected == 0 {
				c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Client with ID %d not found", id)})
				return
			}
			replication.ReplicateToSlaves("delete", "clients", map[string]interface{}{"id": id})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported smart query format for change request."})
			return
		}
	}

	// Remove from pending list
	PendingRequests = append(PendingRequests[:targetIndex], PendingRequests[targetIndex+1:]...)
	log.Printf("[INFO] Change request approved: %s", body.ID)

	c.JSON(http.StatusOK, gin.H{"message": "Request approved and executed successfully"})
}

func rejectChangeRequest(c *gin.Context) {
	var body struct {
		ID string `json:"id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	reqMu.Lock()
	defer reqMu.Unlock()

	var targetIndex = -1
	for i, r := range PendingRequests {
		if r.ID == body.ID {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Request not found"})
		return
	}

	// Remove from pending list
	PendingRequests = append(PendingRequests[:targetIndex], PendingRequests[targetIndex+1:]...)
	log.Printf("[INFO] Change request rejected: %s", body.ID)

	c.JSON(http.StatusOK, gin.H{"message": "Request rejected successfully"})
}

func parseWithdrawQuery(text string) (float64, int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	parts := strings.Split(text, " ")
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("query too short (e.g. withdraw 500 from client 7)")
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid withdrawal amount: %s", parts[1])
	}

	var id int
	var foundID bool
	for i := len(parts) - 1; i >= 0; i-- {
		val, err := strconv.Atoi(parts[i])
		if err == nil {
			id = val
			foundID = true
			break
		}
	}

	if !foundID {
		return 0, 0, fmt.Errorf("could not locate client ID in query")
	}

	return amount, id, nil
}

func parseDeleteQuery(text string) (int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	parts := strings.Split(text, " ")
	if len(parts) < 2 {
		return 0, fmt.Errorf("query too short (e.g. delete client 5)")
	}

	for i := len(parts) - 1; i >= 0; i-- {
		val, err := strconv.Atoi(parts[i])
		if err == nil {
			return val, nil
		}
	}
	return 0, fmt.Errorf("could not locate client ID in query")
}

func parseDepositQuery(text string) (float64, int, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	parts := strings.Split(text, " ")
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("query too short (e.g. deposit 500 to client 7)")
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid deposit amount: %s", parts[1])
	}

	var id int
	var foundID bool
	for i := len(parts) - 1; i >= 0; i-- {
		val, err := strconv.Atoi(parts[i])
		if err == nil {
			id = val
			foundID = true
			break
		}
	}

	if !foundID {
		return 0, 0, fmt.Errorf("could not locate client ID in query")
	}

	return amount, id, nil
}

// ValidateClientData checks business rules before writing to database
func ValidateClientData(name string, balance float64, city string) error {
	if balance < 0 {
		return fmt.Errorf("balance cannot be negative ($%0.2f)", balance)
	}

	for _, r := range city {
		if unicode.IsDigit(r) {
			return fmt.Errorf("city name '%s' cannot contain numeric digits", city)
		}
	}
	return nil
}

